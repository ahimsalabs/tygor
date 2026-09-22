// Package provider implements input providers that extract type information
// from Go code and convert it to the intermediate representation.
package provider

import (
	"context"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
	"tygor.dev/tygorgen/ir"
)

// SourceProvider extracts types by analyzing Go source code.
type SourceProvider struct{}

// normalizePkgPath normalizes a package path for consistency with reflect.
// For "package main", reflect.Type.PkgPath() returns "main", but go/types
// returns the actual module path. This normalizes to "main" for consistency.
func normalizePkgPath(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	if pkg.Name() == "main" {
		return "main"
	}
	return pkg.Path()
}

func isJSONNumberType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "encoding/json" && named.Obj().Name() == "Number"
}

// RootType identifies a type to extract with its package context.
type RootType struct {
	// Name is the type name (e.g., "User", "Page").
	// For generic instantiations, this should be the base name without type parameters.
	Name string

	// Package is the full package path (e.g., "github.com/foo/api").
	Package string
}

// SourceInputOptions configures source-based type extraction.
type SourceInputOptions struct {
	// Packages are the Go package paths to analyze.
	Packages []string

	// RootTypes are the types to extract with package context.
	// If empty, all exported types in the packages are extracted.
	RootTypes []RootType
}

// TypeExpressionResolver converts a reflected Go type expression into its
// source-derived wire descriptor. currentPackage is the package identity used
// for unqualified named types in expression.
type TypeExpressionResolver func(expression, currentPackage string) (ir.TypeDescriptor, error)

// BuildSchema analyzes source code and returns a Schema.
// The provider recursively extracts all types reachable from RootTypes.
func (p *SourceProvider) BuildSchema(ctx context.Context, opts SourceInputOptions) (*ir.Schema, error) {
	schema, _, err := p.BuildSchemaWithResolver(ctx, opts)
	return schema, err
}

// BuildSchemaWithResolver analyzes source code and also returns a resolver
// backed by the same loaded go/types packages. The resolver is used for
// concrete generic endpoint arguments whose reflected spelling does not retain
// enough information to determine encoding/json/v2 wire behavior.
func (p *SourceProvider) BuildSchemaWithResolver(ctx context.Context, opts SourceInputOptions) (*ir.Schema, TypeExpressionResolver, error) {
	builder, err := p.buildSchema(ctx, opts)
	if err != nil {
		return nil, nil, err
	}
	resolver := func(expression, currentPackage string) (ir.TypeDescriptor, error) {
		return builder.resolveTypeExpression(expression, currentPackage)
	}
	return builder.schema, resolver, nil
}

func (p *SourceProvider) buildSchema(ctx context.Context, opts SourceInputOptions) (*schemaBuilder, error) {
	if len(opts.Packages) == 0 {
		return nil, fmt.Errorf("no packages specified")
	}

	// Load packages using go/packages
	cfg := &packages.Config{
		Context: ctx,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo,
	}

	pkgs, err := packages.Load(cfg, opts.Packages...)
	if err != nil {
		return nil, fmt.Errorf("failed to load packages: %w", err)
	}

	// Check for errors in loaded packages
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("package %s has errors: %v", pkg.PkgPath, pkg.Errors)
		}
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found")
	}

	// Create a builder to extract types
	builder := &schemaBuilder{
		pkgs:           pkgs,
		schema:         &ir.Schema{},
		seen:           make(map[types.Type]bool),
		namedTypes:     make(map[string]ir.TypeDescriptor),
		enumCandidates: make(map[*types.Named][]enumConstant),
		typeNames:      make(map[string]bool),
	}

	// Resolve the selector through the loaded package metadata. Package patterns
	// such as "." and "./api" are selectors, not canonical package identities.
	mainPkg, err := builder.resolvePackage(opts.Packages[0])
	if err != nil {
		return nil, err
	}
	// Get the actual directory from the package's files
	pkgDir := mainPkg.PkgPath // fallback to package path
	if len(mainPkg.GoFiles) > 0 {
		pkgDir = filepath.Dir(mainPkg.GoFiles[0])
	}
	builder.schema.Package = ir.PackageInfo{
		Path: mainPkg.PkgPath,
		Name: mainPkg.Name,
		Dir:  pkgDir,
	}

	// Find and process root types
	if len(opts.RootTypes) > 0 {
		// Extract specific root types
		for _, root := range opts.RootTypes {
			if err := builder.extractRootType(root); err != nil {
				return nil, fmt.Errorf("failed to extract root type %s: %w", root.Name, err)
			}
		}
	} else {
		// Extract all exported types
		if err := builder.extractAllExportedTypes(); err != nil {
			return nil, fmt.Errorf("failed to extract exported types: %w", err)
		}
	}

	return builder, nil
}

// schemaBuilder accumulates types and manages the extraction process.
type schemaBuilder struct {
	pkgs           []*packages.Package
	schema         *ir.Schema
	seen           map[types.Type]bool
	namedTypes     map[string]ir.TypeDescriptor // key: pkgPath.Name
	enumCandidates map[*types.Named][]enumConstant
	typeNames      map[string]bool // track used type names for collision detection (§3.5)
}

// enumConstant represents a const declaration that might be an enum member.
type enumConstant struct {
	name  string
	value constant.Value
	obj   *types.Const // Store the const object for doc extraction
}

type sourceJSONField struct {
	field             *types.Var
	tag               string
	name              string
	opts              []string
	index             []int
	tagged            bool
	omitIfEmbeddedNil bool
}

func (b *schemaBuilder) resolveTypeExpression(expression, currentPackage string) (ir.TypeDescriptor, error) {
	typ, err := b.parseTypeExpression(strings.TrimSpace(expression), currentPackage)
	if err != nil {
		return nil, err
	}
	descriptor, err := b.convertType(typ)
	if err != nil {
		return nil, fmt.Errorf("convert type expression %q: %w", expression, err)
	}
	return descriptor, nil
}

func (b *schemaBuilder) parseTypeExpression(expression, currentPackage string) (types.Type, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, fmt.Errorf("empty type expression")
	}
	if expression == "interface{}" || expression == "interface {}" {
		return types.NewInterfaceType(nil, nil).Complete(), nil
	}
	if object := types.Universe.Lookup(expression); object != nil {
		if typeName, ok := object.(*types.TypeName); ok {
			return typeName.Type(), nil
		}
	}
	if strings.HasPrefix(expression, "*") {
		element, err := b.parseTypeExpression(expression[1:], currentPackage)
		if err != nil {
			return nil, err
		}
		return types.NewPointer(element), nil
	}
	if strings.HasPrefix(expression, "[]") {
		element, err := b.parseTypeExpression(expression[2:], currentPackage)
		if err != nil {
			return nil, err
		}
		return types.NewSlice(element), nil
	}
	if strings.HasPrefix(expression, "map[") {
		end := matchingTypeExpressionBracket(expression, 3)
		if end < 0 || end == len(expression)-1 {
			return nil, fmt.Errorf("invalid map type expression %q", expression)
		}
		key, err := b.parseTypeExpression(expression[4:end], currentPackage)
		if err != nil {
			return nil, fmt.Errorf("resolve map key in %q: %w", expression, err)
		}
		value, err := b.parseTypeExpression(expression[end+1:], currentPackage)
		if err != nil {
			return nil, fmt.Errorf("resolve map value in %q: %w", expression, err)
		}
		return types.NewMap(key, value), nil
	}
	if strings.HasPrefix(expression, "[") {
		end := strings.IndexByte(expression, ']')
		if end <= 1 || end == len(expression)-1 {
			return nil, fmt.Errorf("invalid array type expression %q", expression)
		}
		length, err := strconv.ParseInt(expression[1:end], 10, 64)
		if err != nil || length < 0 {
			return nil, fmt.Errorf("invalid array length in type expression %q", expression)
		}
		element, err := b.parseTypeExpression(expression[end+1:], currentPackage)
		if err != nil {
			return nil, err
		}
		return types.NewArray(element, length), nil
	}

	base, arguments := splitTypeExpression(expression)
	baseType, err := b.resolveNamedTypeExpression(base, currentPackage)
	if err != nil {
		return nil, err
	}
	if len(arguments) == 0 {
		return baseType, nil
	}
	typeArguments := make([]types.Type, len(arguments))
	for i, argument := range arguments {
		typeArguments[i], err = b.parseTypeExpression(argument, currentPackage)
		if err != nil {
			return nil, fmt.Errorf("resolve type argument %d in %q: %w", i, expression, err)
		}
	}
	instantiated, err := types.Instantiate(types.NewContext(), baseType, typeArguments, true)
	if err != nil {
		return nil, fmt.Errorf("instantiate type expression %q: %w", expression, err)
	}
	return instantiated, nil
}

func (b *schemaBuilder) resolveNamedTypeExpression(expression, currentPackage string) (types.Type, error) {
	pkgPath, name := splitQualifiedTypeExpression(expression, currentPackage)
	if pkgPath == "" {
		return nil, fmt.Errorf("unresolved type expression %q", expression)
	}
	pkg := b.findLoadedPackage(pkgPath)
	if pkg == nil || pkg.Types == nil {
		return nil, fmt.Errorf("package %s for type expression %q was not loaded", pkgPath, expression)
	}
	object := pkg.Types.Scope().Lookup(name)
	typeName, ok := object.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("type %s not found in package %s", name, pkgPath)
	}
	return typeName.Type(), nil
}

func (b *schemaBuilder) findLoadedPackage(path string) *packages.Package {
	seen := make(map[*packages.Package]bool)
	var find func(*packages.Package) *packages.Package
	find = func(pkg *packages.Package) *packages.Package {
		if pkg == nil || seen[pkg] {
			return nil
		}
		seen[pkg] = true
		if pkg.PkgPath == path || path == "main" && pkg.Name == "main" {
			return pkg
		}
		for _, imported := range pkg.Imports {
			if match := find(imported); match != nil {
				return match
			}
		}
		return nil
	}
	for _, pkg := range b.pkgs {
		if match := find(pkg); match != nil {
			return match
		}
	}
	return nil
}

func matchingTypeExpressionBracket(expression string, open int) int {
	depth := 0
	for i := open; i < len(expression); i++ {
		switch expression[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitTypeExpression(expression string) (string, []string) {
	open := strings.IndexByte(expression, '[')
	if open < 0 || !strings.HasSuffix(expression, "]") {
		return expression, nil
	}
	body := expression[open+1 : len(expression)-1]
	var arguments []string
	start, depth := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				arguments = append(arguments, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	arguments = append(arguments, strings.TrimSpace(body[start:]))
	return strings.TrimSpace(expression[:open]), arguments
}

func splitQualifiedTypeExpression(expression, currentPackage string) (string, string) {
	if dot := strings.LastIndexByte(expression, '.'); dot >= 0 {
		return expression[:dot], expression[dot+1:]
	}
	return currentPackage, expression
}

// extractRootType finds and extracts a named type by name and package.
func (b *schemaBuilder) extractRootType(root RootType) error {
	packagesToSearch := b.pkgs
	if root.Package != "" {
		pkg, err := b.resolvePackage(root.Package)
		if err != nil {
			return err
		}
		packagesToSearch = []*packages.Package{pkg}
	}

	for _, pkg := range packagesToSearch {
		obj := pkg.Types.Scope().Lookup(root.Name)
		if obj == nil {
			continue
		}

		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		if typeName.IsAlias() {
			return fmt.Errorf("go type alias %q is not supported as an explicit root", root.Name)
		}

		if err := b.extractNamedType(typeName); err != nil {
			return err
		}
		return nil
	}
	if root.Package != "" {
		return fmt.Errorf("type %s not found in package %s", root.Name, root.Package)
	}
	return fmt.Errorf("type %s not found in any package", root.Name)
}

func (b *schemaBuilder) resolvePackage(selector string) (*packages.Package, error) {
	var matches []*packages.Package
	for _, pkg := range b.pkgs {
		if pkg.PkgPath == selector || selector == "main" && pkg.Name == "main" {
			matches = append(matches, pkg)
		}
	}

	if len(matches) == 0 && (filepath.IsAbs(selector) || strings.HasPrefix(selector, ".")) {
		selectorDir, err := filepath.Abs(selector)
		if err != nil {
			return nil, fmt.Errorf("resolve package selector %q: %w", selector, err)
		}
		selectorDir = canonicalDir(selectorDir)
		for _, pkg := range b.pkgs {
			if len(pkg.GoFiles) > 0 && canonicalDir(filepath.Dir(pkg.GoFiles[0])) == selectorDir {
				matches = append(matches, pkg)
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("input package %s not found in loaded packages", selector)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("input package %s is ambiguous", selector)
	}
	return matches[0], nil
}

func canonicalDir(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// extractAllExportedTypes extracts all exported types from all packages.
func (b *schemaBuilder) extractAllExportedTypes() error {
	for _, pkg := range b.pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !obj.Exported() {
				continue
			}

			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}

			if err := b.extractNamedType(typeName); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractNamedType extracts a named type and recursively processes dependencies.
func (b *schemaBuilder) extractNamedType(tn *types.TypeName) error {
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil
	}
	if tn.Pkg() != nil && normalizePkgPath(tn.Pkg()) == "time" && tn.Name() == "Duration" {
		return fmt.Errorf("time.Duration has no default encoding/json/v2 representation")
	}

	// Check if already processed
	key := b.typeKey(named)
	if _, exists := b.namedTypes[key]; exists {
		return nil
	}

	hasCustomMarshaler := b.hasCustomMarshaler(named)
	if !hasCustomMarshaler && genericCollectionMayEncodeAsBytes(named) {
		return fmt.Errorf("generic collection %s may use base64 byte encoding for exact byte instantiations; generic byte-capable collection aliases are not supported", tn.Name())
	}

	// Check for name collision (§3.5)
	name := tn.Name()
	pkg := ""
	if named.Obj() != nil && named.Obj().Pkg() != nil {
		pkg = normalizePkgPath(named.Obj().Pkg())
	}
	fullName := pkg + "." + name
	if b.typeNames[fullName] {
		return fmt.Errorf("name collision: type %s already exists", name)
	}
	b.typeNames[fullName] = true

	// Extract documentation and source location
	doc := b.extractDocumentation(tn)
	src := b.extractSource(tn)
	typeParams := b.buildTypeParameters(named)

	// Custom wire encodings take precedence over enum inference.
	if hasCustomMarshaler {
		pkgPath := ""
		if named.Obj() != nil && named.Obj().Pkg() != nil {
			pkgPath = normalizePkgPath(named.Obj().Pkg())
		}
		b.schema.AddWarning(ir.Warning{
			Code:     "CUSTOM_MARSHALER",
			Message:  fmt.Sprintf("type %s implements custom marshaler, mapped to 'unknown'", tn.Name()),
			TypeName: tn.Name(),
		})
		// Create an alias to PrimitiveAny
		aliasDesc := &ir.AliasDescriptor{
			Name:           ir.GoIdentifier{Name: tn.Name(), Package: pkgPath},
			TypeParameters: typeParams,
			Underlying:     ir.Any(),
			Documentation:  doc,
			Source:         src,
		}
		b.namedTypes[key] = aliasDesc
		b.schema.AddType(aliasDesc)
		return nil
	}

	b.scanEnumConstants(tn)
	if consts, isEnum := b.enumCandidates[named]; isEnum && len(consts) > 0 {
		pkgPath := ""
		if named.Obj() != nil && named.Obj().Pkg() != nil {
			pkgPath = normalizePkgPath(named.Obj().Pkg())
		}
		enumDesc := b.buildEnumDescriptor(tn.Name(), pkgPath, consts, doc, src)
		b.namedTypes[key] = enumDesc
		b.schema.AddType(enumDesc)
		return nil
	}

	// Check the underlying type
	switch underlyingType := named.Underlying().(type) {
	case *types.Struct:
		structDesc, err := b.buildStructDescriptor(named, tn.Name(), doc, src)
		if err != nil {
			return err
		}
		b.namedTypes[key] = structDesc
		b.schema.AddType(structDesc)

	case *types.Interface:
		// Interfaces are emitted as PrimitiveAny with a warning
		pkgPath := ""
		if named.Obj() != nil && named.Obj().Pkg() != nil {
			pkgPath = normalizePkgPath(named.Obj().Pkg())
		}
		b.schema.AddWarning(ir.Warning{
			Code:     "INTERFACE_TYPE",
			Message:  fmt.Sprintf("interface type %s mapped to 'unknown'", tn.Name()),
			TypeName: tn.Name(),
		})
		// Create an alias to PrimitiveAny
		aliasDesc := &ir.AliasDescriptor{
			Name:           ir.GoIdentifier{Name: tn.Name(), Package: pkgPath},
			TypeParameters: typeParams,
			Underlying:     ir.Any(),
			Documentation:  doc,
			Source:         src,
		}
		b.namedTypes[key] = aliasDesc
		b.schema.AddType(aliasDesc)

	default:
		// Regular type alias
		underlying, err := b.convertType(underlyingType)
		if err != nil {
			return err
		}
		pkgPath := ""
		if named.Obj() != nil && named.Obj().Pkg() != nil {
			pkgPath = normalizePkgPath(named.Obj().Pkg())
		}
		aliasDesc := &ir.AliasDescriptor{
			Name:           ir.GoIdentifier{Name: tn.Name(), Package: pkgPath},
			TypeParameters: typeParams,
			Underlying:     underlying,
			Documentation:  doc,
			Source:         src,
		}
		b.namedTypes[key] = aliasDesc
		b.schema.AddType(aliasDesc)
	}

	return nil
}

func genericCollectionMayEncodeAsBytes(named *types.Named) bool {
	var element types.Type
	switch collection := named.Underlying().(type) {
	case *types.Slice:
		element = collection.Elem()
	case *types.Array:
		element = collection.Elem()
	default:
		return false
	}
	typeParam, ok := types.Unalias(element).(*types.TypeParam)
	if !ok {
		return false
	}
	return constraintMayAdmitUint8(typeParam.Constraint())
}

func unresolvedSliceElementTypeParam(slice *types.Slice) (*types.TypeParam, bool) {
	typeParam, ok := types.Unalias(slice.Elem()).(*types.TypeParam)
	return typeParam, ok
}

// constraintMayAdmitUint8 reports whether the exact predeclared byte type can
// satisfy a constraint. Defined uint8 types no longer trigger v2 byte encoding.
func constraintMayAdmitUint8(constraint types.Type) bool {
	if constraint == nil {
		return true
	}
	iface, ok := types.Unalias(constraint).Underlying().(*types.Interface)
	return ok && types.Satisfies(types.Typ[types.Byte], iface.Complete())
}

func constraintOnlyAdmitsUint8(constraint types.Type) bool {
	iface, ok := types.Unalias(constraint).Underlying().(*types.Interface)
	if !ok || !types.Satisfies(types.Typ[types.Uint8], iface) {
		return false
	}
	for kind := types.Bool; kind <= types.UnsafePointer; kind++ {
		if kind == types.Uint8 || kind == types.UntypedNil {
			continue
		}
		if types.Satisfies(types.Typ[kind], iface) {
			return false
		}
	}
	return true
}

func (b *schemaBuilder) buildTypeParameters(named *types.Named) []ir.TypeParameterDescriptor {
	tparams := named.TypeParams()
	if tparams == nil || tparams.Len() == 0 {
		return nil
	}

	result := make([]ir.TypeParameterDescriptor, 0, tparams.Len())
	for i := 0; i < tparams.Len(); i++ {
		tp := tparams.At(i)
		var constraint ir.TypeDescriptor
		if constraintType := tp.Constraint(); constraintType != nil {
			constraint = b.convertTypeParamConstraint(constraintType)
		}
		result = append(result, ir.TypeParameterDescriptor{
			ParamName:  tp.Obj().Name(),
			Constraint: constraint,
		})
	}
	return result
}

// typeKey generates a unique key for a named type.
func (b *schemaBuilder) typeKey(named *types.Named) string {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return named.String()
	}
	return normalizePkgPath(obj.Pkg()) + "." + obj.Name()
}

// convertType converts a Go type to an IR TypeDescriptor.
func (b *schemaBuilder) convertType(t types.Type) (ir.TypeDescriptor, error) {
	if named, ok := types.Unalias(t).(*types.Named); ok && named.Obj() != nil && named.Obj().Pkg() != nil &&
		normalizePkgPath(named.Obj().Pkg()) == "time" && named.Obj().Name() == "Duration" {
		return nil, fmt.Errorf("time.Duration has no default encoding/json/v2 representation")
	}
	// Handle special cases first
	if desc := b.handleSpecialType(t); desc != nil {
		return desc, nil
	}

	switch typ := t.(type) {
	case *types.Basic:
		return b.convertBasicType(typ)

	case *types.Named:
		// Reference to a named type - ensure it's extracted first
		obj := typ.Obj()
		pkgPath := ""
		if obj.Pkg() != nil {
			pkgPath = normalizePkgPath(obj.Pkg())
		}
		// Extract the named type if not already processed or being processed
		// (handles recursive types like type Node struct { Next *Node })
		key := b.typeKey(typ)
		if _, exists := b.namedTypes[key]; !exists {
			// Also check typeNames to handle currently-being-processed types
			fullName := pkgPath + "." + obj.Name()
			if !b.typeNames[fullName] {
				if err := b.extractNamedType(obj); err != nil {
					return nil, err
				}
			}
		}

		if args := typ.TypeArgs(); args != nil && args.Len() > 0 {
			if genericInstantiationChangesByteSliceWire(typ.Origin().Underlying(), typ.Underlying(), make(map[typePair]bool)) {
				return nil, fmt.Errorf("generic type %s instantiated with byte-slice-compatible arguments changes JSON to base64 byte-slice encoding", obj.Name())
			}
			typeArgs := make([]ir.TypeDescriptor, args.Len())
			for i := 0; i < args.Len(); i++ {
				arg, err := b.convertType(args.At(i))
				if err != nil {
					return nil, fmt.Errorf("convert type argument %d for %s: %w", i, obj.Name(), err)
				}
				typeArgs[i] = arg
			}
			if err := b.validatePrimitiveWireTypeArguments(typ, typeArgs); err != nil {
				return nil, err
			}
			return ir.RefWithArgs(obj.Name(), pkgPath, typeArgs...), nil
		}
		return ir.Ref(obj.Name(), pkgPath), nil

	case *types.Pointer:
		elem, err := b.convertType(typ.Elem())
		if err != nil {
			return nil, err
		}
		return ir.Ptr(elem), nil

	case *types.Slice:
		if typeParam, ok := unresolvedSliceElementTypeParam(typ); ok && constraintOnlyAdmitsUint8(typeParam.Constraint()) {
			return nil, fmt.Errorf("slice of unresolved type parameter %s always encodes as a base64 byte slice; generic byte slices are not supported", typeParam.Obj().Name())
		}
		elem, err := b.convertType(typ.Elem())
		if err != nil {
			return nil, err
		}
		return ir.Slice(elem), nil

	case *types.Array:
		if typeParam, ok := types.Unalias(typ.Elem()).(*types.TypeParam); ok && constraintOnlyAdmitsUint8(typeParam.Constraint()) {
			return nil, fmt.Errorf("array of unresolved type parameter %s can encode as base64 for exact byte instantiations; generic byte arrays are not supported", typeParam.Obj().Name())
		}
		if isJSONByteArray(typ) {
			return ir.ByteArray(int(typ.Len())), nil
		}
		elem, err := b.convertType(typ.Elem())
		if err != nil {
			return nil, err
		}
		return ir.Array(elem, int(typ.Len())), nil

	case *types.Map:
		key, err := b.convertMapKeyType(typ.Key())
		if err != nil {
			return nil, err
		}
		value, err := b.convertType(typ.Elem())
		if err != nil {
			return nil, err
		}
		// Validate map key type
		if !b.isValidMapKey(typ.Key()) {
			return nil, fmt.Errorf("unsupported map key type: %s", typ.Key())
		}
		return ir.Map(key, value), nil

	case *types.Interface:
		// Empty interface or any
		if typ.Empty() {
			return ir.Any(), nil
		}
		// Non-empty interface
		b.schema.AddWarning(ir.Warning{
			Code:    "INTERFACE_TYPE",
			Message: fmt.Sprintf("interface type %s mapped to 'unknown'", typ.String()),
		})
		return ir.Any(), nil

	case *types.Struct:
		// Anonymous struct - need to generate a synthetic name
		// This is called when processing struct fields, so we don't have
		// context about the parent. Return error - caller should use
		// convertFieldType which has parent context
		return nil, fmt.Errorf("anonymous structs need parent context for synthetic naming")

	case *types.TypeParam:
		// Generic type parameter
		return ir.TypeParam(typ.Obj().Name(), nil), nil

	case *types.Alias:
		// Type alias - follow to the actual type
		return b.convertType(typ.Rhs())

	case *types.Chan, *types.Signature:
		return nil, fmt.Errorf("unsupported type: %s", t.String())

	default:
		return nil, fmt.Errorf("unknown type: %T", t)
	}
}

type typePair struct {
	origin       types.Type
	instantiated types.Type
}

// genericInstantiationChangesByteSliceWire reports whether substituting type
// arguments turns a generic []T or [N]T field into encoding/json/v2's base64 byte
// representation. Generic structs such as Page[T any] are safe to describe,
// but concrete byte-compatible applications do not satisfy that reusable
// TypeScript contract and must be rejected.
func genericInstantiationChangesByteSliceWire(origin, instantiated types.Type, seen map[typePair]bool) bool {
	origin = types.Unalias(origin)
	instantiated = types.Unalias(instantiated)
	pair := typePair{origin: origin, instantiated: instantiated}
	if seen[pair] {
		return false
	}
	seen[pair] = true

	switch original := origin.(type) {
	case *types.Slice:
		actual, ok := instantiated.(*types.Slice)
		if !ok {
			return false
		}
		if _, ok := types.Unalias(original.Elem()).(*types.TypeParam); ok && isJSONByteSlice(actual) {
			return true
		}
		return genericInstantiationChangesByteSliceWire(original.Elem(), actual.Elem(), seen)
	case *types.Array:
		actual, ok := instantiated.(*types.Array)
		if !ok {
			return false
		}
		if _, ok := types.Unalias(original.Elem()).(*types.TypeParam); ok && isJSONByteArray(actual) {
			return true
		}
		return genericInstantiationChangesByteSliceWire(original.Elem(), actual.Elem(), seen)
	case *types.Pointer:
		actual, ok := instantiated.(*types.Pointer)
		return ok && genericInstantiationChangesByteSliceWire(original.Elem(), actual.Elem(), seen)
	case *types.Map:
		actual, ok := instantiated.(*types.Map)
		return ok && genericInstantiationChangesByteSliceWire(original.Elem(), actual.Elem(), seen)
	case *types.Struct:
		actual, ok := instantiated.(*types.Struct)
		if !ok || original.NumFields() != actual.NumFields() {
			return false
		}
		for i := 0; i < original.NumFields(); i++ {
			if genericInstantiationChangesByteSliceWire(original.Field(i).Type(), actual.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Named:
		actual, ok := instantiated.(*types.Named)
		return ok && genericInstantiationChangesByteSliceWire(original.Underlying(), actual.Underlying(), seen)
	}
	return false
}

// validatePrimitiveWireTypeArguments rejects applications whose concrete JSON
// wire type cannot satisfy the constraint retained in generated TypeScript.
// Go validates constraints against the Go type, but special encodings such as
// custom marshalers and json.Number can change that type at the wire boundary.
func (b *schemaBuilder) validatePrimitiveWireTypeArguments(named *types.Named, arguments []ir.TypeDescriptor) error {
	hasPrimitiveArgument := false
	for _, argument := range arguments {
		if _, ok := argument.(*ir.PrimitiveDescriptor); ok {
			hasPrimitiveArgument = true
			break
		}
	}
	if !hasPrimitiveArgument {
		return nil
	}

	descriptor := b.namedTypes[b.typeKey(named)]
	var parameters []ir.TypeParameterDescriptor
	switch descriptor := descriptor.(type) {
	case *ir.StructDescriptor:
		parameters = descriptor.TypeParameters
	case *ir.AliasDescriptor:
		parameters = descriptor.TypeParameters
	default:
		parameters = b.buildTypeParameters(named.Origin())
	}
	if len(parameters) != len(arguments) {
		return fmt.Errorf("type argument metadata for %s has %d parameters, expected %d", named.Obj().Name(), len(parameters), len(arguments))
	}

	bindings := make(wireTypeBindings, len(parameters))
	for i := range parameters {
		bindings[parameters[i].ParamName] = wireTypeBinding{descriptor: arguments[i]}
	}
	typeParameters := named.TypeParams()
	for i, parameter := range parameters {
		argument, ok := arguments[i].(*ir.PrimitiveDescriptor)
		if !ok || parameter.Constraint == nil || b.wireConstraintAcceptsPrimitive(parameter.Constraint, argument, bindings, make(map[ir.GoIdentifier]bool)) {
			continue
		}

		constraint := parameter.ParamName
		if typeParameters != nil && i < typeParameters.Len() {
			constraint = typeParameters.At(i).Constraint().String()
		}
		genericName := named.Obj().Name()
		if pkg := named.Obj().Pkg(); pkg != nil {
			genericName = pkg.Path() + "." + genericName
		}
		return fmt.Errorf(
			"type argument %d (%s) for %s parameter %s has JSON wire type %s, incompatible with preserved constraint %s",
			i,
			named.TypeArgs().At(i).String(),
			genericName,
			parameter.ParamName,
			wirePrimitiveCategory(argument.PrimitiveKind),
			constraint,
		)
	}
	return nil
}

func (b *schemaBuilder) wireConstraintAcceptsPrimitive(
	constraint ir.TypeDescriptor,
	argument *ir.PrimitiveDescriptor,
	bindings wireTypeBindings,
	activeAliases map[ir.GoIdentifier]bool,
) bool {
	switch constraint := constraint.(type) {
	case *ir.PrimitiveDescriptor:
		return constraint.PrimitiveKind == ir.PrimitiveAny ||
			wirePrimitiveCategory(constraint.PrimitiveKind) == wirePrimitiveCategory(argument.PrimitiveKind)
	case *ir.TypeParameterDescriptor:
		binding, ok := bindings[constraint.ParamName]
		return ok && b.wireConstraintAcceptsPrimitive(binding.descriptor, argument, binding.bindings, activeAliases)
	case *ir.UnionDescriptor:
		for _, member := range constraint.Types {
			if b.wireConstraintAcceptsPrimitive(member, argument, bindings, activeAliases) {
				return true
			}
		}
	case *ir.ReferenceDescriptor:
		if activeAliases[constraint.Target] {
			return false
		}
		underlying, aliasBindings, ok := b.wireConstraintAlias(constraint, bindings)
		if !ok {
			return false
		}
		activeAliases[constraint.Target] = true
		accepted := b.wireConstraintAcceptsPrimitive(underlying, argument, aliasBindings, activeAliases)
		delete(activeAliases, constraint.Target)
		return accepted
	case *ir.PtrDescriptor:
		return b.wireConstraintAcceptsPrimitive(constraint.Element, argument, bindings, activeAliases)
	}
	return false
}

func (b *schemaBuilder) wireConstraintAlias(
	reference *ir.ReferenceDescriptor,
	bindings wireTypeBindings,
) (ir.TypeDescriptor, wireTypeBindings, bool) {
	alias, ok := b.schema.FindType(reference.Target).(*ir.AliasDescriptor)
	if !ok {
		return nil, nil, false
	}

	aliasBindings := make(wireTypeBindings, len(alias.TypeParameters))
	for i, parameter := range alias.TypeParameters {
		if i < len(reference.TypeArguments) {
			aliasBindings[parameter.ParamName] = wireTypeBinding{
				descriptor: reference.TypeArguments[i],
				bindings:   bindings,
			}
		}
	}
	return alias.Underlying, aliasBindings, true
}

type wireTypeBinding struct {
	descriptor ir.TypeDescriptor
	bindings   wireTypeBindings
}

type wireTypeBindings map[string]wireTypeBinding

func wirePrimitiveCategory(kind ir.PrimitiveKind) string {
	switch kind {
	case ir.PrimitiveInt, ir.PrimitiveUint, ir.PrimitiveFloat, ir.PrimitiveDuration:
		return "number"
	case ir.PrimitiveString, ir.PrimitiveBytes, ir.PrimitiveTime:
		return "string"
	case ir.PrimitiveBool:
		return "boolean"
	case ir.PrimitiveAny:
		return "unknown"
	case ir.PrimitiveEmpty:
		return "object"
	default:
		return kind.String()
	}
}

func (b *schemaBuilder) convertMapKeyType(t types.Type) (ir.TypeDescriptor, error) {
	// json.Number values are numeric JSON tokens, but map keys are always JSON
	// object member names and encoding/json/v2 accepts arbitrary Number key text.
	if isJSONNumberType(t) {
		return ir.String(), nil
	}
	return b.convertType(t)
}

// handleSpecialType handles special types like time.Time, []byte, etc.
func (b *schemaBuilder) handleSpecialType(t types.Type) ir.TypeDescriptor {
	if isJSONNumberType(t) {
		// json.Number emits an unquoted numeric token. Float64 is the closest
		// existing IR representation, though consumers may lose precision/range.
		return ir.Float(64)
	}

	switch typ := t.(type) {
	case *types.Slice:
		if isJSONByteSlice(typ) {
			return ir.Bytes()
		}

	case *types.Named:
		obj := typ.Obj()
		if obj == nil || obj.Pkg() == nil {
			return nil
		}

		pkgPath := normalizePkgPath(obj.Pkg())
		name := obj.Name()

		// time.Time
		if pkgPath == "time" && name == "Time" {
			return ir.Time()
		}

		// json.RawMessage
		if pkgPath == "encoding/json" && name == "RawMessage" {
			return ir.Any()
		}

		// Check for custom marshalers
		if b.hasCustomMarshaler(typ) {
			b.schema.AddWarning(ir.Warning{
				Code:     "CUSTOM_MARSHALER",
				Message:  fmt.Sprintf("type %s implements custom marshaler, mapped to 'unknown'", name),
				TypeName: name,
			})
			return ir.Any()
		}
	}

	return nil
}

// hasCustomMarshaler checks if a type implements json.Marshaler or encoding.TextMarshaler.
func (b *schemaBuilder) hasCustomMarshaler(named *types.Named) bool {
	for _, receiver := range []types.Type{named, types.NewPointer(named)} {
		if hasCustomMarshalerMethod(receiver) {
			return true
		}
	}
	return false
}

func hasCustomMarshalerMethod(receiver types.Type) bool {
	methods := types.NewMethodSet(receiver)
	for i := 0; i < methods.Len(); i++ {
		method, ok := methods.At(i).Obj().(*types.Func)
		if !ok {
			continue
		}
		switch method.Name() {
		case "MarshalJSON", "MarshalText":
			if isBytesErrorMethod(method) {
				return true
			}
		case "AppendText":
			if isAppendTextMethod(method) {
				return true
			}
		case "MarshalJSONTo":
			if isJSONTextCodecMethod(method, "Encoder") {
				return true
			}
		case "UnmarshalJSON", "UnmarshalText":
			if isBytesInputErrorMethod(method) {
				return true
			}
		case "UnmarshalJSONFrom":
			if isJSONTextCodecMethod(method, "Decoder") {
				return true
			}
		}
	}
	return false
}

func isAppendTextMethod(method *types.Func) bool {
	sig, ok := method.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 2 {
		return false
	}
	byteSlice := types.NewSlice(types.Typ[types.Byte])
	return types.Identical(types.Unalias(sig.Params().At(0).Type()), byteSlice) &&
		types.Identical(types.Unalias(sig.Results().At(0).Type()), byteSlice) &&
		isErrorType(sig.Results().At(1).Type())
}

func isBytesInputErrorMethod(method *types.Func) bool {
	sig, ok := method.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 1 {
		return false
	}
	byteSlice := types.NewSlice(types.Typ[types.Byte])
	return types.Identical(types.Unalias(sig.Params().At(0).Type()), byteSlice) && isErrorType(sig.Results().At(0).Type())
}

func isJSONTextCodecMethod(method *types.Func, parameterName string) bool {
	sig, ok := method.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 1 || !isErrorType(sig.Results().At(0).Type()) {
		return false
	}
	parameter, ok := types.Unalias(sig.Params().At(0).Type()).(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(parameter.Elem()).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == parameterName && normalizePkgPath(named.Obj().Pkg()) == "encoding/json/jsontext"
}

func isErrorType(t types.Type) bool {
	return types.Identical(types.Unalias(t), types.Universe.Lookup("error").Type())

}

func isBytesErrorMethod(method *types.Func) bool {
	sig, ok := method.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 2 {
		return false
	}
	byteSlice := types.NewSlice(types.Typ[types.Byte])
	return types.Identical(types.Unalias(sig.Results().At(0).Type()), byteSlice) &&
		isErrorType(sig.Results().At(1).Type())
}

// isJSONByteSlice mirrors encoding/json/v2's byte-slice selection. Only the
// exact predeclared byte element type (including aliases) uses base64.
func isJSONByteSlice(slice *types.Slice) bool {
	return types.Identical(types.Unalias(slice.Elem()), types.Typ[types.Byte])
}

func isJSONByteArray(array *types.Array) bool {
	return types.Identical(types.Unalias(array.Elem()), types.Typ[types.Byte])
}

// isValidMapKey checks if a type is a valid JSON map key.
func (b *schemaBuilder) isValidMapKey(t types.Type) bool {
	switch typ := t.(type) {
	case *types.Basic:
		kind := typ.Kind()
		// V2 accepts string and numeric key types.
		return kind == types.String ||
			kind >= types.Int && kind <= types.Uintptr ||
			kind == types.Float32 || kind == types.Float64

	case *types.Named:
		// Check underlying type and TextMarshaler
		if b.hasTextMarshaler(typ) {
			return true
		}
		return b.isValidMapKey(typ.Underlying())

	default:
		return false
	}
}

// hasTextMarshaler checks the v2 text encoding interfaces used for map keys.
func (b *schemaBuilder) hasTextMarshaler(named *types.Named) bool {
	methods := types.NewMethodSet(named)
	for i := 0; i < methods.Len(); i++ {
		method, ok := methods.At(i).Obj().(*types.Func)
		if ok {
			switch method.Name() {
			case "MarshalText":
				if isBytesErrorMethod(method) {
					return true
				}
			case "AppendText":
				if isAppendTextMethod(method) {
					return true
				}
			}
		}
	}
	return false
}

// convertBasicType converts a Go basic type to an IR primitive.
func (b *schemaBuilder) convertBasicType(basic *types.Basic) (ir.TypeDescriptor, error) {
	switch basic.Kind() {
	case types.Bool:
		return ir.Bool(), nil
	case types.String:
		return ir.String(), nil
	case types.Int:
		return ir.Int(0), nil
	case types.Int8:
		return ir.Int(8), nil
	case types.Int16:
		return ir.Int(16), nil
	case types.Int32:
		return ir.Int(32), nil
	case types.Int64:
		return ir.Int(64), nil
	case types.Uint, types.Uintptr:
		return ir.Uint(0), nil
	case types.Uint8: // types.Byte is an alias for Uint8
		return ir.Uint(8), nil
	case types.Uint16:
		return ir.Uint(16), nil
	case types.Uint32:
		return ir.Uint(32), nil
	case types.Uint64:
		return ir.Uint(64), nil
	case types.Float32:
		return ir.Float(32), nil
	case types.Float64:
		return ir.Float(64), nil
	case types.UntypedNil:
		return ir.Any(), nil
	case types.Complex64, types.Complex128, types.UnsafePointer:
		return nil, fmt.Errorf("unsupported basic type: %s", basic.String())
	default:
		return nil, fmt.Errorf("unsupported basic type: %s", basic.String())
	}
}

// extractDocumentation extracts documentation from an object.
func (b *schemaBuilder) extractDocumentation(obj types.Object) ir.Documentation {
	// Find the declaration in the AST
	for _, pkg := range b.pkgs {
		if pkg.Types != obj.Pkg() {
			continue
		}

		// Find the object's position
		pos := obj.Pos()
		for _, file := range pkg.Syntax {
			if file.Pos() > pos || file.End() < pos {
				continue
			}

			// Search for the declaration
			var docGroup *ast.CommentGroup
			ast.Inspect(file, func(n ast.Node) bool {
				switch decl := n.(type) {
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						if ts, ok := spec.(*ast.TypeSpec); ok {
							if ts.Name.Pos() == pos {
								docGroup = decl.Doc
								if docGroup == nil {
									docGroup = ts.Doc
								}
								return false
							}
						}
					}
				}
				return true
			})

			if docGroup != nil {
				return b.parseDocumentation(docGroup)
			}
		}
	}

	return ir.Documentation{}
}

// parseDocumentation parses a comment group into Documentation.
func (b *schemaBuilder) parseDocumentation(cg *ast.CommentGroup) ir.Documentation {
	if cg == nil {
		return ir.Documentation{}
	}

	text := cg.Text()
	lines := strings.Split(strings.TrimSpace(text), "\n")

	var summary string
	var deprecated *string

	// Check for deprecated marker
	for i, line := range lines {
		if strings.HasPrefix(line, "Deprecated:") {
			msg := strings.TrimSpace(strings.TrimPrefix(line, "Deprecated:"))
			deprecated = &msg
			// Remove this line from the body
			lines = append(lines[:i], lines[i+1:]...)
			break
		}
	}

	// First non-empty line is the summary
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			summary = trimmed
			break
		}
	}

	body := strings.Join(lines, "\n")

	return ir.Documentation{
		Summary:    summary,
		Body:       body,
		Deprecated: deprecated,
	}
}

// extractSource extracts source location information.
func (b *schemaBuilder) extractSource(obj types.Object) ir.Source {
	pos := obj.Pos()
	if !pos.IsValid() {
		return ir.Source{}
	}

	for _, pkg := range b.pkgs {
		if pkg.Fset != nil {
			position := pkg.Fset.Position(pos)
			return ir.Source{
				File:   position.Filename,
				Line:   position.Line,
				Column: position.Column,
			}
		}
	}

	return ir.Source{}
}

// extractConstDocumentation extracts documentation for a const declaration.
func (b *schemaBuilder) extractConstDocumentation(cnst *types.Const) ir.Documentation {
	if cnst == nil {
		return ir.Documentation{}
	}

	pos := cnst.Pos()
	if !pos.IsValid() {
		return ir.Documentation{}
	}

	for _, pkg := range b.pkgs {
		if pkg.Types != cnst.Pkg() {
			continue
		}

		for _, file := range pkg.Syntax {
			if file.Pos() > pos || file.End() < pos {
				continue
			}

			// Search for the const declaration
			var docGroup *ast.CommentGroup
			ast.Inspect(file, func(n ast.Node) bool {
				if decl, ok := n.(*ast.GenDecl); ok && decl.Tok == token.CONST {
					for _, spec := range decl.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok {
							for _, name := range vs.Names {
								if name.Pos() == pos {
									// Found the const - prefer spec doc, fall back to decl doc
									docGroup = vs.Doc
									if docGroup == nil {
										docGroup = decl.Doc
									}
									return false
								}
							}
						}
					}
				}
				return true
			})

			if docGroup != nil {
				return b.parseDocumentation(docGroup)
			}
		}
	}

	return ir.Documentation{}
}

// extractFieldDocumentation extracts documentation for a struct field.
func (b *schemaBuilder) extractFieldDocumentation(structObj types.Object, fieldPos token.Pos) ir.Documentation {
	if structObj == nil || !fieldPos.IsValid() {
		return ir.Documentation{}
	}

	for _, pkg := range b.pkgs {
		if pkg.Types != structObj.Pkg() {
			continue
		}

		for _, file := range pkg.Syntax {
			if file.Pos() > fieldPos || file.End() < fieldPos {
				continue
			}

			// Search for the field in the struct declaration
			var docGroup *ast.CommentGroup
			ast.Inspect(file, func(n ast.Node) bool {
				if ts, ok := n.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						for _, f := range st.Fields.List {
							for _, name := range f.Names {
								if name.Pos() == fieldPos {
									docGroup = f.Doc
									return false
								}
							}
						}
					}
				}
				return true
			})

			if docGroup != nil {
				return b.parseDocumentation(docGroup)
			}
		}
	}

	return ir.Documentation{}
}

// scanEnumConstants scans for const declarations that might be enum members.
func (b *schemaBuilder) scanEnumConstants(tn *types.TypeName) {
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return
	}

	// Only consider defined types with primitive underlying types
	underlying := named.Underlying()
	if _, ok := underlying.(*types.Basic); !ok {
		return
	}

	pkg := tn.Pkg()
	if pkg == nil {
		return
	}

	// Scan all const declarations in the package
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		cnst, ok := obj.(*types.Const)
		if !ok {
			continue
		}

		// Check if const has the same type as our named type
		// Only include exported constants - unexported constants are internal
		// implementation details and shouldn't appear in generated code
		if types.Identical(cnst.Type(), named) && cnst.Exported() {
			b.enumCandidates[named] = append(b.enumCandidates[named], enumConstant{
				name:  cnst.Name(),
				value: cnst.Val(),
				obj:   cnst,
			})
		}
	}
}

// buildEnumDescriptor creates an EnumDescriptor from constants.
func (b *schemaBuilder) buildEnumDescriptor(name, pkgPath string, consts []enumConstant, doc ir.Documentation, src ir.Source) *ir.EnumDescriptor {
	members := make([]ir.EnumMember, len(consts))
	for i, c := range consts {
		value := b.constantValue(c.value)
		members[i] = ir.EnumMember{
			Name:          c.name,
			Value:         value,
			Documentation: b.extractConstDocumentation(c.obj),
		}
	}

	return &ir.EnumDescriptor{
		Name:          ir.GoIdentifier{Name: name, Package: pkgPath},
		Members:       members,
		Documentation: doc,
		Source:        src,
	}
}

// constantValue converts a constant.Value to string, int64, or float64.
func (b *schemaBuilder) constantValue(v constant.Value) any {
	switch v.Kind() {
	case constant.String:
		return constant.StringVal(v)
	case constant.Int:
		i64, _ := constant.Int64Val(v)
		return i64
	case constant.Float:
		f64, _ := constant.Float64Val(v)
		return f64
	case constant.Bool:
		return constant.BoolVal(v)
	default:
		return v.String()
	}
}

// buildStructDescriptor creates a StructDescriptor from a named struct type.
func (b *schemaBuilder) buildStructDescriptor(named *types.Named, name string, doc ir.Documentation, src ir.Source) (*ir.StructDescriptor, error) {
	structType, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("not a struct type")
	}

	pkgPath := ""
	if named.Obj() != nil && named.Obj().Pkg() != nil {
		pkgPath = normalizePkgPath(named.Obj().Pkg())
	}

	descriptor := &ir.StructDescriptor{
		Name:           ir.GoIdentifier{Name: name, Package: pkgPath},
		TypeParameters: b.buildTypeParameters(named),
		Fields:         []ir.FieldDescriptor{},
		Documentation:  doc,
		Source:         src,
	}

	fields, err := b.sourceJSONFields(structType)
	if err != nil {
		return nil, err
	}
	for _, field := range fields {
		fieldDesc, err := b.buildSourceFieldDescriptor(field, name, pkgPath)
		if err != nil {
			return nil, err
		}
		descriptor.Fields = append(descriptor.Fields, fieldDesc)
	}

	return descriptor, nil
}

func (b *schemaBuilder) sourceJSONFields(root *types.Struct) ([]sourceJSONField, error) {
	type scan struct {
		typ               types.Type
		strct             *types.Struct
		index             []int
		omitIfEmbeddedNil bool
	}

	current := []scan(nil)
	next := []scan{{typ: root, strct: root}}
	var count, nextCount map[types.Type]int
	visited := make(map[types.Type]bool)
	var fields []sourceJSONField

	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, make(map[types.Type]int)

		for _, parent := range current {
			if visited[parent.typ] {
				continue
			}
			visited[parent.typ] = true
			names := make(map[string]string)

			for i := 0; i < parent.strct.NumFields(); i++ {
				field := parent.strct.Field(i)
				embeddedType, embeddedStruct, isEmbeddedStruct := sourceEmbeddedStruct(field.Type())
				if field.Embedded() {
					if !field.Exported() && !isEmbeddedStruct {
						continue
					}
				} else if !field.Exported() {
					continue
				}

				tag := parent.strct.Tag(i)
				rawJSONTag := b.extractTag(tag, "json")
				if rawJSONTag == "-" {
					continue
				}
				name, opts := b.parseJSONTag(tag)
				if name != "" && !isValidJSONTag(name) {
					return nil, fmt.Errorf("field %s has invalid encoding/json/v2 object name %q", field.Name(), name)
				}
				index := append(append([]int(nil), parent.index...), i)
				for _, opt := range opts {
					switch {
					case opt == "embed":
						return nil, fmt.Errorf("field %s: explicit encoding/json/v2 embed fields are not supported", field.Name())
					case strings.HasPrefix(opt, "format:"):
						return nil, fmt.Errorf("field %s: encoding/json/v2 does not support %q", field.Name(), opt)
					}
				}
				if field.Embedded() && name == "" {
					if !isEmbeddedStruct {
						return nil, fmt.Errorf("embedded non-struct field %s must have an explicit JSON name under encoding/json/v2", field.Name())
					}
					if len(opts) > 0 {
						return nil, fmt.Errorf("embedded field %s cannot have options without an explicit JSON name under encoding/json/v2", field.Name())
					}
				}

				if name != "" || !field.Embedded() || !isEmbeddedStruct {
					tagged := name != ""
					if name == "" {
						name = field.Name()
					}
					if previous, ok := names[name]; ok {
						return nil, fmt.Errorf("fields %s and %s conflict over JSON object name %q under encoding/json/v2", previous, field.Name(), name)
					}
					names[name] = field.Name()
					candidate := sourceJSONField{
						field:             field,
						tag:               tag,
						name:              name,
						opts:              opts,
						index:             index,
						tagged:            tagged,
						omitIfEmbeddedNil: parent.omitIfEmbeddedNil,
					}
					fields = append(fields, candidate)
					if count[parent.typ] > 1 {
						fields = append(fields, candidate)
					}
					continue
				}

				nextCount[embeddedType]++
				if nextCount[embeddedType] == 1 {
					next = append(next, scan{
						typ:               embeddedType,
						strct:             embeddedStruct,
						index:             index,
						omitIfEmbeddedNil: parent.omitIfEmbeddedNil || isSourcePointer(field.Type()),
					})
				}
			}
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].name != fields[j].name {
			return fields[i].name < fields[j].name
		}
		if len(fields[i].index) != len(fields[j].index) {
			return len(fields[i].index) < len(fields[j].index)
		}
		if fields[i].tagged != fields[j].tagged {
			return fields[i].tagged
		}
		return compareFieldIndex(fields[i].index, fields[j].index) < 0
	})

	out := fields[:0]
	for i := 0; i < len(fields); {
		end := i + 1
		for end < len(fields) && fields[end].name == fields[i].name {
			end++
		}
		if end-i == 1 || len(fields[i].index) != len(fields[i+1].index) || fields[i].tagged != fields[i+1].tagged {
			out = append(out, fields[i])
		}
		i = end
	}

	sort.Slice(out, func(i, j int) bool {
		return compareFieldIndex(out[i].index, out[j].index) < 0
	})
	return out, nil
}

func sourceEmbeddedStruct(t types.Type) (types.Type, *types.Struct, bool) {
	t = types.Unalias(t)
	if pointer, ok := t.(*types.Pointer); ok {
		t = types.Unalias(pointer.Elem())
	}
	switch typ := t.(type) {
	case *types.Named:
		strct, ok := typ.Underlying().(*types.Struct)
		return typ, strct, ok
	case *types.Struct:
		return typ, typ, true
	default:
		return t, nil, false
	}
}

func isSourcePointer(t types.Type) bool {
	_, ok := types.Unalias(t).(*types.Pointer)
	return ok
}

func compareFieldIndex(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func isValidJSONTag(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if strings.ContainsRune(",\\'\"`", r) {
			return false
		}
	}
	return true
}

func (b *schemaBuilder) buildSourceFieldDescriptor(field sourceJSONField, parentName, pkgPath string) (ir.FieldDescriptor, error) {
	fieldType, err := b.convertFieldType(field.field.Type(), parentName, field.field.Name(), pkgPath)
	if err != nil {
		return ir.FieldDescriptor{}, fmt.Errorf("failed to convert field %s: %w", field.field.Name(), err)
	}

	omitEmpty := false
	omitZero := false
	stringEncodingRequested := false
	for _, opt := range field.opts {
		switch {
		case opt == "omitempty":
			omitEmpty = true
		case opt == "omitzero":
			omitZero = true
		case opt == "string":
			stringEncodingRequested = true
		case opt == "embed":
			return ir.FieldDescriptor{}, fmt.Errorf("field %s: explicit encoding/json/v2 embed fields are not supported", field.field.Name())
		case strings.HasPrefix(opt, "format:"):
			return ir.FieldDescriptor{}, fmt.Errorf("field %s: encoding/json/v2 does not support %q", field.field.Name(), opt)
		}
	}
	if stringEncodingRequested {
		if typeParam := unresolvedStringEncodingTypeParameter(field.field.Type()); typeParam != nil {
			return ir.FieldDescriptor{}, fmt.Errorf(
				`field %s: json:",string" on unresolved type parameter %s is not supported`,
				field.field.Name(), typeParam.Obj().Name(),
			)
		}
		if !b.schema.StringEncodingApplies(fieldType) {
			return ir.FieldDescriptor{}, fmt.Errorf("field %s: encoding/json/v2 option string only applies to numeric types", field.field.Name())
		}
	}

	return ir.FieldDescriptor{
		Name:              field.field.Name(),
		Type:              fieldType,
		JSONName:          field.name,
		OmitEmpty:         omitEmpty,
		OmitZero:          omitZero,
		OmitIfEmbeddedNil: field.omitIfEmbeddedNil,
		StringEncoded:     stringEncodingRequested,
		ValidateTag:       b.extractTag(field.tag, "validate"),
		RawTags:           b.parseAllTags(field.tag),
		Documentation:     b.extractFieldDocumentation(field.field, field.field.Pos()),
	}, nil
}

func unresolvedStringEncodingTypeParameter(t types.Type) *types.TypeParam {
	for {
		t = types.Unalias(t)
		pointer, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = pointer.Elem()
	}
	t = types.Unalias(t)
	typeParam, _ := t.(*types.TypeParam)
	return typeParam
}

// parseJSONTag parses the json struct tag.
func (b *schemaBuilder) parseJSONTag(tag string) (name string, opts []string) {
	jsonTag := b.extractTag(tag, "json")
	if jsonTag == "" {
		return "", nil
	}

	parts := strings.Split(jsonTag, ",")
	name = parts[0]
	if len(parts) > 1 {
		opts = parts[1:]
	}
	return
}

// extractTag extracts a specific tag value from a struct tag.
func (b *schemaBuilder) extractTag(tag, key string) string {
	// Parse struct tag
	st := parseStructTag(tag)
	return st[key]
}

// parseAllTags parses all struct tags into a map.
func (b *schemaBuilder) parseAllTags(tag string) map[string]string {
	return parseStructTag(tag)
}

// convertTypeParamConstraint converts a type parameter constraint to IR.
func (b *schemaBuilder) convertTypeParamConstraint(constraint types.Type) ir.TypeDescriptor {
	if constraint == nil {
		return nil
	}

	// Check the string representation first for special cases
	constraintStr := constraint.String()

	// "any" and "comparable" are special built-in constraints
	// Per spec §3.4, these are NOT preserved in the IR
	if constraintStr == "any" || constraintStr == "comparable" {
		return nil
	}

	// Handle type aliases (e.g., "any" which is an alias to interface{})
	if alias, ok := constraint.(*types.Alias); ok {
		// Follow the alias to its actual type
		underlying := alias.Rhs()
		underlyingStr := underlying.String()
		if underlyingStr == "interface{}" || underlyingStr == "any" {
			return nil
		}
		// Recursively check the underlying type
		return b.convertTypeParamConstraint(underlying)
	}

	// Handle named interface constraints (e.g., type Stringish interface { ~string | ~int })
	// We need to look at the underlying interface to extract union type sets
	if named, ok := constraint.(*types.Named); ok {
		underlying := named.Underlying()
		if iface, ok := underlying.(*types.Interface); ok {
			// Recursively process the underlying interface to extract union type sets
			result := b.convertTypeParamConstraint(iface)
			if result != nil {
				return result
			}
			// If the interface has methods but no union type set, preserve the
			// named constraint and extract its declaration like any other reference.
			if iface.NumMethods() > 0 {
				desc, err := b.convertType(named)
				if err == nil {
					return desc
				}
				b.schema.AddWarning(ir.Warning{
					Code:     "CONSTRAINT_CONVERSION_FAILED",
					Message:  fmt.Sprintf("failed to convert type parameter constraint %s: %v", constraint.String(), err),
					TypeName: constraint.String(),
				})
				return nil
			}
		}
		// Fall through to convertType for other named types
	}

	// Check for interface constraints
	iface, ok := constraint.(*types.Interface)
	if !ok {
		// Non-interface constraint - try to convert
		desc, err := b.convertType(constraint)
		if err == nil {
			return desc
		}
		// Conversion failed - add a warning and return nil (unconstrained)
		b.schema.AddWarning(ir.Warning{
			Code:     "CONSTRAINT_CONVERSION_FAILED",
			Message:  fmt.Sprintf("failed to convert type parameter constraint %s: %v", constraint.String(), err),
			TypeName: constraint.String(),
		})
		return nil
	}

	// Handle interface{} / any (empty interface)
	if iface.Empty() {
		return nil
	}

	// Check if this is the "comparable" interface
	ifaceStr := iface.String()
	if ifaceStr == "comparable" || ifaceStr == "interface{comparable}" {
		return nil
	}

	// Extract union constraints from interface type sets
	// For [T ~string | ~int], the interface has embedded types that form a union
	if iface.NumEmbeddeds() > 0 {
		var unionTypes []ir.TypeDescriptor
		conversionFailed := false

		for i := 0; i < iface.NumEmbeddeds(); i++ {
			embedded := iface.EmbeddedType(i)

			// Check if this is a union type (e.g., ~string | ~int)
			if union, ok := embedded.(*types.Union); ok {
				for j := 0; j < union.Len(); j++ {
					term := union.Term(j)
					// term.Tilde() indicates ~T (approximation), but for IR purposes
					// we only care about the underlying type since JSON behavior is the same
					termType := term.Type()
					desc, err := b.convertType(termType)
					if err == nil && desc != nil {
						unionTypes = append(unionTypes, desc)
					} else {
						conversionFailed = true
						if err == nil {
							err = fmt.Errorf("no representable descriptor")
						}
						b.schema.AddWarning(ir.Warning{
							Code:     "UNION_TERM_CONVERSION_FAILED",
							Message:  fmt.Sprintf("failed to convert union term %s: %v", termType.String(), err),
							TypeName: termType.String(),
						})
					}
				}
			} else {
				// Single embedded type (not a union)
				desc, err := b.convertType(embedded)
				if err == nil && desc != nil {
					unionTypes = append(unionTypes, desc)
				} else {
					conversionFailed = true
					if err == nil {
						err = fmt.Errorf("no representable descriptor")
					}
					b.schema.AddWarning(ir.Warning{
						Code:     "CONSTRAINT_EMBEDDED_CONVERSION_FAILED",
						Message:  fmt.Sprintf("failed to convert embedded constraint type %s: %v", embedded.String(), err),
						TypeName: embedded.String(),
					})
				}
			}
		}
		if conversionFailed {
			return nil
		}

		if len(unionTypes) > 1 {
			return ir.Union(unionTypes...)
		} else if len(unionTypes) == 1 {
			return unionTypes[0]
		}
	}

	return nil
}

// convertFieldType converts a field type, handling anonymous structs with parent context.
// For anonymous structs, it generates a synthetic name and adds the struct to Schema.Types.
func (b *schemaBuilder) convertFieldType(t types.Type, parentName, fieldName, pkgPath string) (ir.TypeDescriptor, error) {
	// Check if this is an anonymous struct
	if structType, ok := t.(*types.Struct); ok {
		return b.handleAnonymousStruct(structType, parentName, fieldName, pkgPath)
	}

	// Check if this is a pointer to an anonymous struct
	if ptr, ok := t.(*types.Pointer); ok {
		if structType, ok := ptr.Elem().(*types.Struct); ok {
			// Handle the anonymous struct, then wrap in Ptr
			innerDesc, err := b.handleAnonymousStruct(structType, parentName, fieldName, pkgPath)
			if err != nil {
				return nil, err
			}
			return ir.Ptr(innerDesc), nil
		}
	}

	// For non-struct types, use regular conversion
	return b.convertType(t)
}

// handleAnonymousStruct generates a synthetic name for an anonymous struct and adds it to Schema.Types.
func (b *schemaBuilder) handleAnonymousStruct(structType *types.Struct, parentName, fieldName, pkgPath string) (ir.TypeDescriptor, error) {
	// Generate synthetic name: ParentType_FieldName (§3.5)
	syntheticName := parentName + "_" + fieldName

	// Check for name collision (§3.5)
	fullKey := pkgPath + "." + syntheticName
	if b.typeNames[fullKey] {
		return nil, fmt.Errorf("name collision: synthetic name %s already exists", syntheticName)
	}

	// Mark this synthetic name as used
	b.typeNames[fullKey] = true

	// Build the struct descriptor for the anonymous struct
	descriptor := &ir.StructDescriptor{
		Name:   ir.GoIdentifier{Name: syntheticName, Package: pkgPath},
		Fields: []ir.FieldDescriptor{},
	}

	fields, err := b.sourceJSONFields(structType)
	if err != nil {
		return nil, err
	}
	for _, field := range fields {
		fieldDesc, err := b.buildSourceFieldDescriptor(field, syntheticName, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field %s.%s: %w", syntheticName, field.field.Name(), err)
		}
		descriptor.Fields = append(descriptor.Fields, fieldDesc)
	}

	// Add the synthetic struct to Schema.Types
	b.namedTypes[fullKey] = descriptor
	b.schema.AddType(descriptor)

	// Return a reference to the synthetic type
	return ir.Ref(syntheticName, pkgPath), nil
}

// parseStructTag parses a struct tag string into a map.
func parseStructTag(tag string) map[string]string {
	result := make(map[string]string)
	for tag != "" {
		// Skip leading space
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Find key
		i = 0
		for i < len(tag) && tag[i] != ':' && tag[i] != ' ' {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' {
			break
		}
		key := tag[:i]
		tag = tag[i+1:]

		// Find value (quoted string)
		if tag[0] != '"' {
			break
		}
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		value, err := strconv.Unquote(tag[:i+1])
		if err != nil {
			break
		}
		tag = tag[i+1:]

		if _, exists := result[key]; !exists {
			result[key] = value
		}
	}
	return result
}
