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
	"strings"

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

// BuildSchema analyzes source code and returns a Schema.
// The provider recursively extracts all types reachable from RootTypes.
func (p *SourceProvider) BuildSchema(ctx context.Context, opts SourceInputOptions) (*ir.Schema, error) {
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

	// Populate package info from first INPUT package (not first loaded package)
	// packages.Load returns packages in dependency order, not input order
	var mainPkg *packages.Package
	firstInput := opts.Packages[0]
	for _, pkg := range pkgs {
		// "." is special: packages.Load(".") returns the package with its real path,
		// not ".". So we match on "." by finding a package whose directory matches cwd,
		// or just take the first non-dependency package.
		if firstInput == "." {
			// For ".", use the first package that was explicitly requested
			// packages.Load returns requested packages first, then dependencies
			mainPkg = pkg
			break
		}
		if pkg.PkgPath == firstInput {
			mainPkg = pkg
			break
		}
	}
	if mainPkg == nil {
		return nil, fmt.Errorf("input package %s not found in loaded packages", firstInput)
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

	return builder.schema, nil
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

// extractRootType finds and extracts a named type by name and package.
func (b *schemaBuilder) extractRootType(root RootType) error {
	for _, pkg := range b.pkgs {
		// If package is specified, only look in that package
		// Special case: "main" from reflect matches any package named "main"
		if root.Package != "" {
			if root.Package == "main" {
				if pkg.Name != "main" {
					continue
				}
			} else if pkg.PkgPath != root.Package {
				continue
			}
		}

		obj := pkg.Types.Scope().Lookup(root.Name)
		if obj == nil {
			continue
		}

		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
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

	// Check if already processed
	key := b.typeKey(named)
	if _, exists := b.namedTypes[key]; exists {
		return nil
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

	// First, scan for enum constants before processing the type
	b.scanEnumConstants(tn)

	// Extract documentation and source location
	doc := b.extractDocumentation(tn)
	src := b.extractSource(tn)

	// Check if this is an enum type
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

	// Check for custom marshalers on the named type itself
	if b.hasCustomMarshaler(named) {
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
			Name:          ir.GoIdentifier{Name: tn.Name(), Package: pkgPath},
			Underlying:    ir.Any(),
			Documentation: doc,
			Source:        src,
		}
		b.namedTypes[key] = aliasDesc
		b.schema.AddType(aliasDesc)
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
			Name:          ir.GoIdentifier{Name: tn.Name(), Package: pkgPath},
			Underlying:    ir.Any(),
			Documentation: doc,
			Source:        src,
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
			Name:          ir.GoIdentifier{Name: tn.Name(), Package: pkgPath},
			Underlying:    underlying,
			Documentation: doc,
			Source:        src,
		}
		b.namedTypes[key] = aliasDesc
		b.schema.AddType(aliasDesc)
	}

	return nil
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
		return ir.Ref(obj.Name(), pkgPath), nil

	case *types.Pointer:
		elem, err := b.convertType(typ.Elem())
		if err != nil {
			return nil, err
		}
		return ir.Ptr(elem), nil

	case *types.Slice:
		elem, err := b.convertType(typ.Elem())
		if err != nil {
			return nil, err
		}
		return ir.Slice(elem), nil

	case *types.Array:
		elem, err := b.convertType(typ.Elem())
		if err != nil {
			return nil, err
		}
		return ir.Array(elem, int(typ.Len())), nil

	case *types.Map:
		key, err := b.convertType(typ.Key())
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

// handleSpecialType handles special types like time.Time, []byte, etc.
func (b *schemaBuilder) handleSpecialType(t types.Type) ir.TypeDescriptor {
	switch typ := t.(type) {
	case *types.Slice:
		// Check for []byte or []uint8
		if basic, ok := typ.Elem().(*types.Basic); ok {
			if basic.Kind() == types.Byte || basic.Kind() == types.Uint8 {
				return ir.Bytes()
			}
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

		// time.Duration
		if pkgPath == "time" && name == "Duration" {
			return ir.Duration()
		}

		// json.Number
		if pkgPath == "encoding/json" && name == "Number" {
			return ir.String()
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
	// Look for MarshalJSON() ([]byte, error) method
	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		if method.Name() == "MarshalJSON" {
			sig := method.Type().(*types.Signature)
			if sig.Params().Len() == 0 && sig.Results().Len() == 2 {
				// Check return types: first must be []byte, second must be error
				results := sig.Results()
				firstType := results.At(0).Type()
				secondType := results.At(1).Type()

				// Check first result is []byte
				if slice, ok := firstType.(*types.Slice); ok {
					if basic, ok := slice.Elem().(*types.Basic); ok {
						if basic.Kind() == types.Byte || basic.Kind() == types.Uint8 {
							// Check second result is error
							if secondType.String() == "error" {
								return true
							}
						}
					}
				}
			}
		}
		if method.Name() == "MarshalText" {
			sig := method.Type().(*types.Signature)
			if sig.Params().Len() == 0 && sig.Results().Len() == 2 {
				// Check return types: first must be []byte, second must be error
				results := sig.Results()
				firstType := results.At(0).Type()
				secondType := results.At(1).Type()

				// Check first result is []byte
				if slice, ok := firstType.(*types.Slice); ok {
					if basic, ok := slice.Elem().(*types.Basic); ok {
						if basic.Kind() == types.Byte || basic.Kind() == types.Uint8 {
							// Check second result is error
							if secondType.String() == "error" {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

// isValidMapKey checks if a type is a valid JSON map key.
func (b *schemaBuilder) isValidMapKey(t types.Type) bool {
	switch typ := t.(type) {
	case *types.Basic:
		kind := typ.Kind()
		// String and integer types are valid
		return kind == types.String ||
			kind >= types.Int && kind <= types.Uint64

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

// hasTextMarshaler checks if a type implements encoding.TextMarshaler.
func (b *schemaBuilder) hasTextMarshaler(named *types.Named) bool {
	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		if method.Name() == "MarshalText" {
			sig := method.Type().(*types.Signature)
			if sig.Params().Len() == 0 && sig.Results().Len() == 2 {
				// Check return types: first must be []byte, second must be error
				results := sig.Results()
				firstType := results.At(0).Type()
				secondType := results.At(1).Type()

				// Check first result is []byte
				if slice, ok := firstType.(*types.Slice); ok {
					if basic, ok := slice.Elem().(*types.Basic); ok {
						if basic.Kind() == types.Byte || basic.Kind() == types.Uint8 {
							// Check second result is error
							if secondType.String() == "error" {
								return true
							}
						}
					}
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

	// Extract type parameters for generic types
	var typeParams []ir.TypeParameterDescriptor
	if tparams := named.TypeParams(); tparams != nil && tparams.Len() > 0 {
		for i := 0; i < tparams.Len(); i++ {
			tp := tparams.At(i)
			// tp.Constraint() returns the constraint interface
			// For [T any], this returns a non-nil interface but it's the universal constraint
			// We need to check if it's effectively unconstrained
			constraintType := tp.Constraint()
			var constraint ir.TypeDescriptor
			if constraintType != nil {
				constraint = b.convertTypeParamConstraint(constraintType)
			}
			typeParams = append(typeParams, ir.TypeParameterDescriptor{
				ParamName:  tp.Obj().Name(),
				Constraint: constraint,
			})
		}
	}

	descriptor := &ir.StructDescriptor{
		Name:           ir.GoIdentifier{Name: name, Package: pkgPath},
		TypeParameters: typeParams,
		Fields:         []ir.FieldDescriptor{},
		Extends:        []ir.GoIdentifier{},
		Documentation:  doc,
		Source:         src,
	}

	// Process struct fields
	for i := 0; i < structType.NumFields(); i++ {
		field := structType.Field(i)
		tag := structType.Tag(i)

		// Skip unexported fields
		if !field.Exported() {
			continue
		}

		// Parse struct tags
		jsonTag, jsonOpts := b.parseJSONTag(tag)

		// Skip fields with json:"-"
		if jsonTag == "-" {
			continue
		}

		// Handle embedded fields
		if field.Embedded() {
			if jsonTag == "" {
				// No JSON tag - this is inheritance (Extends)
				fieldType := field.Type()
				// Dereference pointer
				if ptr, ok := fieldType.(*types.Pointer); ok {
					fieldType = ptr.Elem()
				}
				if named, ok := fieldType.(*types.Named); ok {
					obj := named.Obj()
					embeddedPkgPath := ""
					if obj != nil && obj.Pkg() != nil {
						embeddedPkgPath = normalizePkgPath(obj.Pkg())
					}
					descriptor.Extends = append(descriptor.Extends, ir.GoIdentifier{
						Name:    obj.Name(),
						Package: embeddedPkgPath,
					})
				}
				continue
			}
			// Has JSON tag - treat as regular field
		}

		// Convert field type - use convertFieldType to handle anonymous structs
		fieldTypeDesc, err := b.convertFieldType(field.Type(), name, field.Name(), pkgPath)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field %s: %w", field.Name(), err)
		}

		// Determine JSON name
		jsonName := jsonTag
		if jsonName == "" {
			jsonName = field.Name()
		}

		// Check for omitempty/omitzero
		optional := false
		for _, opt := range jsonOpts {
			if opt == "omitempty" || opt == "omitzero" {
				optional = true
				break
			}
		}

		// Check for string encoding
		stringEncoded := false
		for _, opt := range jsonOpts {
			if opt == "string" {
				stringEncoded = true
				break
			}
		}

		// Extract validate tag
		validateTag := b.extractTag(tag, "validate")

		fieldDesc := ir.FieldDescriptor{
			Name:          field.Name(),
			Type:          fieldTypeDesc,
			JSONName:      jsonName,
			Optional:      optional,
			StringEncoded: stringEncoded,
			Skip:          false,
			ValidateTag:   validateTag,
			RawTags:       b.parseAllTags(tag),
			Documentation: b.extractFieldDocumentation(named.Obj(), field.Pos()),
		}

		descriptor.Fields = append(descriptor.Fields, fieldDesc)
	}

	return descriptor, nil
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
			// If the interface has methods but no union type set, return a reference
			// to the named constraint type (e.g., [T Stringer] -> Ref("Stringer"))
			if iface.NumMethods() > 0 {
				obj := named.Obj()
				pkgPath := ""
				if obj != nil && obj.Pkg() != nil {
					pkgPath = normalizePkgPath(obj.Pkg())
				}
				return ir.Ref(obj.Name(), pkgPath)
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
					} else if err != nil {
						// Log warning but continue processing other union terms
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
				} else if err != nil {
					// Log warning but continue processing
					b.schema.AddWarning(ir.Warning{
						Code:     "CONSTRAINT_EMBEDDED_CONVERSION_FAILED",
						Message:  fmt.Sprintf("failed to convert embedded constraint type %s: %v", embedded.String(), err),
						TypeName: embedded.String(),
					})
				}
			}
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
		Name:    ir.GoIdentifier{Name: syntheticName, Package: pkgPath},
		Fields:  []ir.FieldDescriptor{},
		Extends: []ir.GoIdentifier{},
	}

	// Process fields of the anonymous struct
	for i := 0; i < structType.NumFields(); i++ {
		field := structType.Field(i)
		tag := structType.Tag(i)

		// Skip unexported fields
		if !field.Exported() {
			continue
		}

		// Parse struct tags
		jsonTag, jsonOpts := b.parseJSONTag(tag)

		// Skip fields with json:"-"
		if jsonTag == "-" {
			continue
		}

		// Handle embedded fields
		if field.Embedded() {
			if jsonTag == "" {
				// No JSON tag - this is inheritance (Extends)
				fieldType := field.Type()
				// Dereference pointer
				if ptr, ok := fieldType.(*types.Pointer); ok {
					fieldType = ptr.Elem()
				}
				if named, ok := fieldType.(*types.Named); ok {
					obj := named.Obj()
					embeddedPkgPath := ""
					if obj != nil && obj.Pkg() != nil {
						embeddedPkgPath = normalizePkgPath(obj.Pkg())
					}
					descriptor.Extends = append(descriptor.Extends, ir.GoIdentifier{
						Name:    obj.Name(),
						Package: embeddedPkgPath,
					})
				}
				continue
			}
			// Has JSON tag - treat as regular field
		}

		// Convert field type - use recursive call for nested anonymous structs
		fieldTypeDesc, err := b.convertFieldType(field.Type(), syntheticName, field.Name(), pkgPath)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field %s.%s: %w", syntheticName, field.Name(), err)
		}

		// Determine JSON name
		jsonName := jsonTag
		if jsonName == "" {
			jsonName = field.Name()
		}

		// Check for omitempty/omitzero
		optional := false
		for _, opt := range jsonOpts {
			if opt == "omitempty" || opt == "omitzero" {
				optional = true
				break
			}
		}

		// Check for string encoding
		stringEncoded := false
		for _, opt := range jsonOpts {
			if opt == "string" {
				stringEncoded = true
				break
			}
		}

		// Extract validate tag
		validateTag := b.extractTag(tag, "validate")

		fieldDesc := ir.FieldDescriptor{
			Name:          field.Name(),
			Type:          fieldTypeDesc,
			JSONName:      jsonName,
			Optional:      optional,
			StringEncoded: stringEncoded,
			Skip:          false,
			ValidateTag:   validateTag,
			RawTags:       b.parseAllTags(tag),
			Documentation: ir.Documentation{}, // Anonymous struct fields don't have documentation positions
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
		value := tag[1:i]
		tag = tag[i+1:]

		result[key] = value
	}
	return result
}
