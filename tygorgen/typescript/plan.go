package typescript

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"tygor.dev/tygorgen/ir"
)

// generationPlan owns every generated symbol and module name. Go identifiers
// remain the identity; TypeScript-safe spellings are presentation only.
type generationPlan struct {
	typeNames            map[ir.GoIdentifier]string
	packageFiles         map[string]string
	recursive            map[ir.GoIdentifier]bool
	unguardedAliasCycles map[ir.GoIdentifier][]ir.GoIdentifier
}

func newGenerationPlan(schema *ir.Schema, config GeneratorConfig) *generationPlan {
	recursive := findRecursiveTypes(schema.Types)
	return &generationPlan{
		typeNames:            allocateTypeNames(schema, config),
		packageFiles:         allocatePackageFiles(schema.Types),
		recursive:            recursive,
		unguardedAliasCycles: findUnguardedAliasCycles(schema, getTypeScriptConfig(config), recursive),
	}
}

func allocateTypeNames(schema *ir.Schema, config GeneratorConfig) map[ir.GoIdentifier]string {
	ids := make([]ir.GoIdentifier, 0, len(schema.Types))
	for _, typ := range schema.Types {
		ids = append(ids, typ.TypeName())
	}
	sortIdentifiers(ids)

	preferred := make(map[ir.GoIdentifier]string, len(ids))
	groups := make(map[string][]ir.GoIdentifier)
	for _, id := range ids {
		name := preferredTypeName(id, schema.Package.Path, config)
		preferred[id] = name
		groups[name] = append(groups[name], id)
	}

	allocated := make(map[ir.GoIdentifier]string, len(ids))
	reserved := make(map[string]bool, len(ids))
	for _, id := range ids {
		name := preferred[id]
		if len(groups[name]) == 1 {
			allocated[id] = name
			reserved[name] = true
		}
	}

	for _, id := range ids {
		if _, ok := allocated[id]; ok {
			continue
		}
		qualifier := packageQualifier(id.Package, config.StripPackagePrefix)
		candidate := reserveGeneratedTypeName(sanitizeTypeScriptIdentifier(qualifier + "_" + applyNameTransforms(id.Name, config)))
		if qualifier == "" || reserved[candidate] {
			candidate = candidate + "_" + identitySuffix(id.Package+"\x00"+id.Name)
		}
		for reserved[candidate] {
			candidate += "_"
		}
		allocated[id] = candidate
		reserved[candidate] = true
	}

	return allocated
}

func preferredTypeName(id ir.GoIdentifier, mainPackage string, config GeneratorConfig) string {
	name := applyNameTransforms(id.Name, config)
	if config.StripPackagePrefix != "" && id.Package != "" && id.Package != mainPackage {
		if qualifier := packageQualifier(id.Package, config.StripPackagePrefix); qualifier != "" {
			name = qualifier + "_" + name
		}
	}
	return reserveGeneratedTypeName(sanitizeTypeScriptIdentifier(name))
}

func reserveGeneratedTypeName(name string) string {
	name = escapeReservedWord(name)
	if name == "Record" {
		return name + "_"
	}
	return name
}

func packageQualifier(pkg, stripPrefix string) string {
	if stripPrefix != "" {
		pkg = strings.TrimPrefix(pkg, stripPrefix)
	}
	return sanitizePkgPath(pkg)
}

func sanitizeTypeScriptIdentifier(name string) string {
	var b strings.Builder
	for i, r := range name {
		valid := r == '_' || r == '$' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r))
		if valid {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	result := b.String()
	if result == "" {
		return "_"
	}
	return result
}

func allocatePackageFiles(types []ir.TypeDescriptor) map[string]string {
	packages := make(map[string]bool)
	for _, typ := range types {
		packages[typ.TypeName().Package] = true
	}

	ordered := make([]string, 0, len(packages))
	for pkg := range packages {
		ordered = append(ordered, pkg)
	}
	sort.Strings(ordered)

	files := make(map[string]string, len(ordered))
	reserved := make(map[string]bool, len(ordered))
	for _, pkg := range ordered {
		component := sanitizePkgPath(pkg)
		if component == "" {
			component = "root"
		}
		filename := "types_" + component + ".ts"
		key := strings.ToLower(filename)
		if reserved[key] {
			filename = "types_" + component + "_" + identitySuffix(pkg) + ".ts"
			key = strings.ToLower(filename)
			for reserved[key] {
				filename = strings.TrimSuffix(filename, ".ts") + "_.ts"
				key = strings.ToLower(filename)
			}
		}
		files[pkg] = filename
		reserved[key] = true
	}
	return files
}

func identitySuffix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:4])
}

func sortIdentifiers(ids []ir.GoIdentifier) {
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].Package == ids[j].Package {
			return ids[i].Name < ids[j].Name
		}
		return ids[i].Package < ids[j].Package
	})
}

func collectDescriptorReferences(td ir.TypeDescriptor) []ir.GoIdentifier {
	seen := make(map[ir.GoIdentifier]bool)
	var walk func(ir.TypeDescriptor)
	walk = func(current ir.TypeDescriptor) {
		if current == nil {
			return
		}
		switch t := current.(type) {
		case *ir.StructDescriptor:
			for _, ext := range t.Extends {
				seen[ext] = true
			}
			for _, field := range t.Fields {
				if !field.Skip {
					walk(field.Type)
				}
			}
			for i := range t.TypeParameters {
				walk(t.TypeParameters[i].Constraint)
			}
		case *ir.AliasDescriptor:
			walk(t.Underlying)
			for i := range t.TypeParameters {
				walk(t.TypeParameters[i].Constraint)
			}
		case *ir.ReferenceDescriptor:
			seen[t.Target] = true
			for _, arg := range t.TypeArguments {
				walk(arg)
			}
		case *ir.ArrayDescriptor:
			walk(t.Element)
		case *ir.MapDescriptor:
			walk(t.Key)
			walk(t.Value)
		case *ir.PtrDescriptor:
			walk(t.Element)
		case *ir.UnionDescriptor:
			for _, member := range t.Types {
				walk(member)
			}
		case *ir.TypeParameterDescriptor:
			walk(t.Constraint)
		}
	}
	walk(td)

	refs := make([]ir.GoIdentifier, 0, len(seen))
	for id := range seen {
		refs = append(refs, id)
	}
	sortIdentifiers(refs)
	return refs
}

func findRecursiveTypes(types []ir.TypeDescriptor) map[ir.GoIdentifier]bool {
	known := make(map[ir.GoIdentifier]bool, len(types))
	graph := make(map[ir.GoIdentifier][]ir.GoIdentifier, len(types))
	for _, typ := range types {
		known[typ.TypeName()] = true
	}
	for _, typ := range types {
		for _, ref := range collectDescriptorReferences(typ) {
			if known[ref] {
				graph[typ.TypeName()] = append(graph[typ.TypeName()], ref)
			}
		}
	}

	recursive := make(map[ir.GoIdentifier]bool)
	for start := range known {
		visited := make(map[ir.GoIdentifier]bool)
		var reachesStart func(ir.GoIdentifier) bool
		reachesStart = func(current ir.GoIdentifier) bool {
			for _, next := range graph[current] {
				if next == start {
					return true
				}
				if !visited[next] {
					visited[next] = true
					if reachesStart(next) {
						return true
					}
				}
			}
			return false
		}
		if reachesStart(start) {
			recursive[start] = true
		}
	}
	return recursive
}

// findUnguardedAliasCycles finds recursive alias declarations whose emitted
// TypeScript RHS eagerly references itself. Pointer and union syntax is
// transparent, while array/tuple syntax and inline object declarations provide
// recursion boundaries. Generic alias arguments are scanned in the caller's
// declaration because TypeScript resolves them eagerly before substitution.
func findUnguardedAliasCycles(schema *ir.Schema, tsConfig TypeScriptConfig, recursive map[ir.GoIdentifier]bool) map[ir.GoIdentifier][]ir.GoIdentifier {
	aliases := make(map[ir.GoIdentifier]*ir.AliasDescriptor)
	for _, descriptor := range schema.Types {
		if alias, ok := descriptor.(*ir.AliasDescriptor); ok {
			aliases[alias.Name] = alias
		}
	}

	graph := make(map[ir.GoIdentifier][]ir.GoIdentifier, len(aliases))
	for id, alias := range aliases {
		if _, ok := unwrapPointers(alias.Underlying).(*ir.MapDescriptor); ok && recursive[id] {
			// Recursive maps beneath explicit pointers use an inline index
			// signature, which is a valid TypeScript recursion boundary.
			continue
		}
		edges := make(map[ir.GoIdentifier]bool)
		var scan func(ir.TypeDescriptor)
		scan = func(descriptor ir.TypeDescriptor) {
			switch current := descriptor.(type) {
			case *ir.PtrDescriptor:
				scan(current.Element)
			case *ir.UnionDescriptor:
				for _, member := range current.Types {
					scan(member)
				}
			case *ir.ArrayDescriptor:
				// Tuple and array syntax guards recursive occurrences.
				return
			case *ir.MapDescriptor:
				// Record is an eager generic alias, so only its value can
				// introduce a recursive declaration dependency.
				scan(current.Value)
			case *ir.ReferenceDescriptor:
				target := schema.FindType(current.Target)
				switch target := target.(type) {
				case *ir.AliasDescriptor:
					edges[target.Name] = true
					for _, argument := range current.TypeArguments {
						scan(argument)
					}
				case *ir.StructDescriptor:
					if !tsConfig.UseInterface || len(target.Extends) > 0 {
						for _, argument := range current.TypeArguments {
							scan(argument)
						}
					}
				}
			}
		}
		scan(alias.Underlying)
		for edge := range edges {
			graph[id] = append(graph[id], edge)
		}
		sortIdentifiers(graph[id])
	}

	cycles := make(map[ir.GoIdentifier][]ir.GoIdentifier)
	ids := make([]ir.GoIdentifier, 0, len(aliases))
	for id := range aliases {
		ids = append(ids, id)
	}
	sortIdentifiers(ids)
	for _, start := range ids {
		visited := make(map[ir.GoIdentifier]bool)
		var find func(ir.GoIdentifier, []ir.GoIdentifier) []ir.GoIdentifier
		find = func(current ir.GoIdentifier, path []ir.GoIdentifier) []ir.GoIdentifier {
			for _, next := range graph[current] {
				if next == start {
					return append(append([]ir.GoIdentifier(nil), path...), start)
				}
				if !visited[next] {
					visited[next] = true
					if cycle := find(next, append(path, next)); len(cycle) > 0 {
						return cycle
					}
				}
			}
			return nil
		}
		visited[start] = true
		if cycle := find(start, []ir.GoIdentifier{start}); len(cycle) > 0 {
			cycles[start] = cycle
		}
	}
	return cycles
}
