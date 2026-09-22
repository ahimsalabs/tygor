//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"tygor.dev/tygor"
	"tygor.dev/tygorgen"
	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/provider"
	"tygor.dev/tygorgen/provider/testdata"
	"tygor.dev/tygorgen/provider/testdata/genericwire"
	"tygor.dev/tygorgen/sink"
	typescript "tygor.dev/tygorgen/typescript"
)

type GeneratorConfig = typescript.GeneratorConfig
type GenerateOptions = typescript.GenerateOptions
type TypeScriptGenerator = typescript.TypeScriptGenerator

type reflectedRecursivePointerAlias *reflectedRecursivePointerAlias

func TestSourceGenericDefinedBytesCompileAndRun(t *testing.T) {
	requireTypeScriptToolchain(t)
	type byteResponse = testdata.Response[[]testdata.Octet]
	type customResponse = testdata.Response[[]testdata.MarshaledOctet]
	type nestedResponse = testdata.Response[map[string][][]testdata.Octet]
	type phantomResponse = testdata.Phantom[[]testdata.Octet]

	app := tygor.NewApp()
	app.Service("Bytes").Exec("Echo", func(context.Context, byteResponse) (byteResponse, error) {
		return byteResponse{}, nil
	})
	app.Service("Bytes").Exec("Custom", func(context.Context, customResponse) (customResponse, error) {
		return customResponse{}, nil
	})
	app.Service("Bytes").Exec("Nested", func(context.Context, nestedResponse) (nestedResponse, error) {
		return nestedResponse{}, nil
	})
	app.Service("Bytes").Exec("Phantom", func(context.Context, phantomResponse) (phantomResponse, error) {
		return phantomResponse{}, nil
	})

	dir := t.TempDir()
	if _, err := tygorgen.Generate(app, &tygorgen.Config{
		OutDir:     dir,
		Provider:   "source",
		SingleFile: true,
		Flavors:    []tygorgen.Flavor{tygorgen.FlavorZod, tygorgen.FlavorZodMini},
	}); err != nil {
		t.Fatal(err)
	}

	bytePayload, err := json.Marshal(byteResponse{Data: []testdata.Octet{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	customPayload, err := json.Marshal(customResponse{Data: []testdata.MarshaledOctet{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	nestedPayload, err := json.Marshal(nestedResponse{Data: map[string][][]testdata.Octet{"items": {{1, 2}}}})
	if err != nil {
		t.Fatal(err)
	}
	phantomPayload, err := json.Marshal(phantomResponse{OK: true})
	if err != nil {
		t.Fatal(err)
	}

	runtime := loadTypeScriptFixture(t, "source-generic-defined-bytes.ts",
		"__BYTE_JSON__", strconv.Quote(string(bytePayload)),
		"__CUSTOM_JSON__", strconv.Quote(string(customPayload)),
		"__NESTED_JSON__", strconv.Quote(string(nestedPayload)),
		"__PHANTOM_JSON__", strconv.Quote(string(phantomPayload)),
	)
	writeContractSupportFiles(t, dir, runtime)
	runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")
	runContractCommand(t, dir, "bun", "run", "runtime.ts")
}

func TestGeneratedZodContractsCompileAndRun(t *testing.T) {
	requireTypeScriptToolchain(t)

	const (
		modelPkg = "example.com/model"
		leftPkg  = "example.com/a-b"
		rightPkg = "example.com/a_b"
	)
	id := func(pkg, name string) ir.GoIdentifier { return ir.GoIdentifier{Package: pkg, Name: name} }
	typeParam := *ir.TypeParam("T", ir.String())

	schema := &ir.Schema{
		Package: ir.PackageInfo{Path: modelPkg, Name: "model"},
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{Name: id(modelPkg, "Record")},
			&ir.StructDescriptor{Name: id(modelPkg, "Base"), Fields: []ir.FieldDescriptor{{Name: "Base", JSONName: "base", Type: ir.String()}}},
			&ir.StructDescriptor{Name: id(modelPkg, "Derived"), Extends: []ir.GoIdentifier{id(modelPkg, "Base")}, Fields: []ir.FieldDescriptor{{Name: "Own", JSONName: "own", Type: ir.String()}}},
			&ir.StructDescriptor{Name: id(leftPkg, "User"), Fields: []ir.FieldDescriptor{{Name: "Left", JSONName: "left", Type: ir.String()}}},
			&ir.StructDescriptor{Name: id(rightPkg, "User"), Fields: []ir.FieldDescriptor{{Name: "Right", JSONName: "right", Type: ir.String()}}},
			&ir.StructDescriptor{Name: id(modelPkg, "Pair"), Fields: []ir.FieldDescriptor{
				{Name: "Left", JSONName: "left", Type: ir.Ref("User", leftPkg)},
				{Name: "Right", JSONName: "right", Type: ir.Ref("User", rightPkg)},
			}},
			&ir.StructDescriptor{Name: id(modelPkg, "Node"), Fields: []ir.FieldDescriptor{
				{Name: "Value", JSONName: "value", Type: ir.String()},
				{Name: "Next", JSONName: "next", Type: ir.Ptr(ir.Ref("Node", modelPkg))},
			}},
			&ir.StructDescriptor{Name: id(modelPkg, "A"), Fields: []ir.FieldDescriptor{{Name: "B", JSONName: "b", Type: ir.Ptr(ir.Ref("B", modelPkg))}}},
			&ir.StructDescriptor{Name: id(modelPkg, "B"), Fields: []ir.FieldDescriptor{{Name: "A", JSONName: "a", Type: ir.Ptr(ir.Ref("A", modelPkg))}}},
			&ir.StructDescriptor{Name: id(modelPkg, "CycleHolder"), Fields: []ir.FieldDescriptor{{Name: "Cycle", JSONName: "cycle", Type: ir.Ref("A", modelPkg)}}},
			&ir.StructDescriptor{Name: id(modelPkg, "AboveCycle"), Fields: []ir.FieldDescriptor{{Name: "Holder", JSONName: "holder", Type: ir.Ref("CycleHolder", modelPkg)}}},
			&ir.StructDescriptor{Name: id(modelPkg, "Tree"), TypeParameters: []ir.TypeParameterDescriptor{typeParam}, Fields: []ir.FieldDescriptor{
				{Name: "Value", JSONName: "value", Type: ir.TypeParam("T", nil)},
				{Name: "Child", JSONName: "child", Type: ir.Ptr(ir.RefWithArgs("Tree", modelPkg, ir.TypeParam("T", nil)))},
			}},
			&ir.AliasDescriptor{Name: id(modelPkg, "RecursiveList"), Underlying: ir.Slice(ir.Ref("RecursiveList", modelPkg))},
			&ir.AliasDescriptor{Name: id(modelPkg, "RecursiveMap"), Underlying: ir.Map(ir.String(), ir.Ref("RecursiveMap", modelPkg))},
			&ir.AliasDescriptor{Name: id(modelPkg, "Names"), Underlying: ir.Slice(ir.String())},
			&ir.AliasDescriptor{Name: id(modelPkg, "Labels"), Underlying: ir.Map(ir.String(), ir.String())},
			&ir.EnumDescriptor{Name: id(modelPkg, "WireStatus"), Members: []ir.EnumMember{{Name: "Ready", Value: int64(1)}, {Name: "Done", Value: int64(3)}}},
			&ir.EnumDescriptor{Name: id(modelPkg, "WireMode"), Members: []ir.EnumMember{{Name: "Red", Value: "red"}, {Name: "Blue", Value: "blue"}, {Name: "Green", Value: "green"}}},
			&ir.AliasDescriptor{Name: id(modelPkg, "StatusAlias"), Underlying: ir.Ref("WireStatus", modelPkg)},
			&ir.StructDescriptor{
				Name:          id(modelPkg, "Contract"),
				Documentation: ir.Documentation{Body: "A terminator */ must stay inside JSDoc."},
				Fields: []ir.FieldDescriptor{
					{Name: "BigID", JSONName: "big_id", Type: ir.Int(64), StringEncoded: true, ValidateTag: "oneof=9223372036854775807"},
					{Name: "UnsignedID", JSONName: "unsigned_id", Type: ir.Uint(64), StringEncoded: true},
					{Name: "Ratio", JSONName: "ratio", Type: ir.Float(64), StringEncoded: true},
					{Name: "Maybe", JSONName: "maybe", Type: ir.Ptr(ir.Int(64)), StringEncoded: true},
					{Name: "Flag", JSONName: "flag", Type: ir.Bool(), StringEncoded: true, ValidateTag: "oneof=false"},
					{Name: "Choice", JSONName: "choice", Type: ir.Int(32), ValidateTag: "oneof=1 2"},
					{Name: "Exact", JSONName: "exact", Type: ir.Int(32), ValidateTag: "len=5"},
					{Name: "Quoted", JSONName: "field-name", Type: ir.String(), ValidateTag: "gte=2,lte=10"},
					{Name: "Encoded", JSONName: "encoded", Type: ir.String(), StringEncoded: true, ValidateTag: "required,email,gte=5"},
					{Name: "EncodedChoice", JSONName: "encoded_choice", Type: ir.String(), StringEncoded: true, ValidateTag: "oneof=red blue"},
					{Name: "Status", JSONName: "status", Type: ir.Ref("WireStatus", modelPkg), StringEncoded: true, ValidateTag: "lte=2"},
					{Name: "StatusNumber", JSONName: "status_number", Type: ir.Ref("WireStatus", modelPkg), ValidateTag: "gte=1,lte=3,oneof=1 3"},
					{Name: "Mode", JSONName: "mode", Type: ir.Ref("WireMode", modelPkg), ValidateTag: "oneof=red blue"},
					{Name: "IP", JSONName: "ip", Type: ir.String(), ValidateTag: "ip"},
					{Name: "IPv4", JSONName: "ipv4", Type: ir.String(), ValidateTag: "ipv4"},
					{Name: "IPv6", JSONName: "ipv6", Type: ir.String(), ValidateTag: "ipv6"},
					{Name: "Items", JSONName: "items", Type: ir.Slice(ir.Ptr(ir.Ref("Node", modelPkg))), ValidateTag: "gte=1"},
					{Name: "Counts", JSONName: "counts", Type: ir.Map(ir.Int(8), ir.String()), ValidateTag: "max=2"},
					{Name: "UnsignedCounts", JSONName: "unsigned_counts", Type: ir.Map(ir.Uint(8), ir.String())},
					{Name: "EnumCounts", JSONName: "enum_counts", Type: ir.Map(ir.Ref("WireStatus", modelPkg), ir.String())},
					{Name: "AliasEnumCounts", JSONName: "alias_enum_counts", Type: ir.Map(ir.Ref("StatusAlias", modelPkg), ir.String())},
					{Name: "Lookup", JSONName: "lookup", Type: ir.Map(ir.String(), ir.Ptr(ir.Ref("Node", modelPkg)))},
					{Name: "Groups", JSONName: "groups", Type: ir.Slice(ir.Slice(ir.String()))},
					{Name: "Bytes", JSONName: "bytes", Type: ir.Bytes()},
					{Name: "ByteGroups", JSONName: "byte_groups", Type: ir.Slice(ir.Bytes())},
					{Name: "EmptyArray", JSONName: "empty_array", Type: ir.Array(ir.String(), 0)},
					{Name: "PairArray", JSONName: "pair_array", Type: ir.Array(ir.Ptr(ir.String()), 2), ValidateTag: "len=2,min=2,max=2"},
					{Name: "Names", JSONName: "names", Type: ir.Ptr(ir.Ref("Names", modelPkg)), ValidateTag: "gte=1"},
					{Name: "Labels", JSONName: "labels", Type: ir.Ptr(ir.Ref("Labels", modelPkg)), ValidateTag: "gte=1"},
					{Name: "Optional", JSONName: "optional", Type: ir.Ptr(ir.String()), Optional: true},
				},
			},
		},
		Services: []ir.ServiceDescriptor{{Name: "Contracts", Endpoints: []ir.EndpointDescriptor{
			{Name: "Get", FullName: "Contracts.Get", Primitive: "query", Path: "/Contracts/Get", Request: ir.String(), Response: ir.Ptr(ir.Ref("Contract", modelPkg))},
			{Name: "Walk", FullName: "Contracts.Walk", Primitive: "query", Path: "/Contracts/Walk", Request: ir.Ref("RecursiveMap", modelPkg), Response: ir.Ref("RecursiveList", modelPkg)},
		}}},
	}

	dir := t.TempDir()
	emitTypes := true
	generateContractFixture(t, dir, schema, GeneratorConfig{
		SingleFile:      false,
		TrailingNewline: true,
		EmitComments:    true,
		Custom: map[string]any{
			"Flavors":   []string{"zod", "zod-mini"},
			"EmitTypes": &emitTypes,
		},
	})
	type wireNode struct {
		Value string    `json:"value"`
		Next  *wireNode `json:"next"`
	}
	type wireContract struct {
		BigID           int64                `json:"big_id,string"`
		UnsignedID      uint64               `json:"unsigned_id,string"`
		Ratio           float64              `json:"ratio,string"`
		Maybe           *int64               `json:"maybe,string"`
		Flag            bool                 `json:"flag,string"`
		Choice          int32                `json:"choice"`
		Exact           int32                `json:"exact"`
		Quoted          string               `json:"field-name"`
		Encoded         string               `json:"encoded,string"`
		EncodedChoice   string               `json:"encoded_choice,string"`
		Status          int64                `json:"status,string"`
		StatusNumber    int64                `json:"status_number"`
		Mode            string               `json:"mode"`
		IP              string               `json:"ip"`
		IPv4            string               `json:"ipv4"`
		IPv6            string               `json:"ipv6"`
		Items           []*wireNode          `json:"items"`
		Counts          map[int8]string      `json:"counts"`
		UnsignedCounts  map[uint8]string     `json:"unsigned_counts"`
		EnumCounts      map[int64]string     `json:"enum_counts"`
		AliasEnumCounts map[int64]string     `json:"alias_enum_counts"`
		Lookup          map[string]*wireNode `json:"lookup"`
		Groups          [][]string           `json:"groups"`
		Bytes           []byte               `json:"bytes"`
		ByteGroups      [][]byte             `json:"byte_groups"`
		EmptyArray      [0]string            `json:"empty_array"`
		PairArray       [2]*string           `json:"pair_array"`
		Names           *[]string            `json:"names"`
		Labels          *map[string]string   `json:"labels"`
		Optional        *string              `json:"optional,omitempty"`
	}
	pairValue := "pair"
	names := []string{"name"}
	labels := map[string]string{"key": "value"}
	payload, err := json.Marshal(wireContract{
		BigID:           9223372036854775807,
		UnsignedID:      18446744073709551615,
		Ratio:           125,
		Flag:            false,
		Choice:          2,
		Exact:           5,
		Quoted:          "ok",
		Encoded:         "person@example.com",
		EncodedChoice:   "red",
		Status:          1,
		StatusNumber:    1,
		Mode:            "red",
		IP:              "2001:db8::1",
		IPv4:            "192.0.2.1",
		IPv6:            "2001:db8::1",
		Items:           []*wireNode{nil, {Value: "child"}},
		Counts:          map[int8]string{3: "three"},
		UnsignedCounts:  map[uint8]string{3: "three"},
		EnumCounts:      map[int64]string{1: "one"},
		AliasEnumCounts: map[int64]string{3: "three"},
		Lookup:          map[string]*wireNode{"missing": nil},
		Groups:          [][]string{nil, {}},
		ByteGroups:      [][]byte{nil, {1, 2}},
		PairArray:       [2]*string{nil, &pairValue},
		Names:           &names,
		Labels:          &labels,
	})
	if err != nil {
		t.Fatal(err)
	}

	runtime := loadTypeScriptFixture(t, "generated-contract.ts", "__WIRE_JSON__", strconv.Quote(string(payload)))
	writeContractSupportFiles(t, dir, runtime)
	runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")
	runContractCommand(t, dir, "bun", "run", "runtime.ts")
}

func TestGeneratedJSONWireParityForStringDepthAndDefinedBytes(t *testing.T) {
	requireTypeScriptToolchain(t)

	schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
		Packages: []string{
			"tygor.dev/tygorgen/provider/testdata",
			"tygor.dev/tygorgen/provider/testdata/genericwire",
		},
		RootTypes: []provider.RootType{
			{Name: "StringEncodingDepths"},
			{Name: "DefinedByteSlices"},
			{Name: "CustomElementByteSlice"},
			{Name: "AliasResultMarshalers"},
			{Package: "tygor.dev/tygorgen/provider/testdata/genericwire", Name: "GenericStringEncodingDepths"},
			{Package: "tygor.dev/tygorgen/provider/testdata/genericwire", Name: "UnconstrainedCustomMarshalerPayload"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	emitTypes := true
	generateContractFixture(t, dir, schema, GeneratorConfig{
		SingleFile:      false,
		TrailingNewline: true,
		Custom: map[string]any{
			"Flavors":   []string{"zod", "zod-mini"},
			"EmitTypes": &emitTypes,
		},
	})

	value := 7
	single := &value
	double := &single
	triple := &double
	depthPayload, err := json.Marshal(testdata.StringEncodingDepths{
		Direct:   value,
		Single:   single,
		Double:   double,
		Triple:   triple,
		Duration: time.Second,
		Defined:  testdata.DefinedIntPointer(single),
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantDepthPayload = `{"direct":"7","single":"7","double":7,"triple":7,"duration":"1000000000","defined":"7"}`
	if string(depthPayload) != wantDepthPayload {
		t.Fatalf("encoding/json pointer-depth wire payload = %s, want %s", depthPayload, wantDepthPayload)
	}
	nilDepthPayload, err := json.Marshal(testdata.StringEncodingDepths{
		Direct:   value,
		Single:   single,
		Double:   double,
		Triple:   triple,
		Duration: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantNilDepthPayload = `{"direct":"7","single":"7","double":7,"triple":7,"duration":"1000000000","defined":null}`
	if string(nilDepthPayload) != wantNilDepthPayload {
		t.Fatalf("encoding/json nil pointer-depth wire payload = %s, want %s", nilDepthPayload, wantNilDepthPayload)
	}
	applied := genericwire.PhantomPointer[string](single)
	doubleApplied := &applied
	genericDepthPayload, err := json.Marshal(genericwire.GenericStringEncodingDepths{
		Applied: applied,
		Double:  doubleApplied,
		Plain:   applied,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantGenericDepthPayload = `{"applied":"7","double":7,"plain":7}`
	if string(genericDepthPayload) != wantGenericDepthPayload {
		t.Fatalf("encoding/json generic pointer-depth wire payload = %s, want %s", genericDepthPayload, wantGenericDepthPayload)
	}
	genericNilDepthPayload, err := json.Marshal(genericwire.GenericStringEncodingDepths{
		Double: doubleApplied,
		Plain:  applied,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantGenericNilDepthPayload = `{"applied":null,"double":7,"plain":7}`
	if string(genericNilDepthPayload) != wantGenericNilDepthPayload {
		t.Fatalf("encoding/json nil generic pointer-depth wire payload = %s, want %s", genericNilDepthPayload, wantGenericNilDepthPayload)
	}
	bytePayload, err := json.Marshal(testdata.DefinedByteSlices{Data: []testdata.Octet{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(bytePayload), `{"data":"AQI="}`; got != want {
		t.Fatalf("encoding/json defined-byte wire payload = %s, want %s", got, want)
	}
	customBytePayload, err := json.Marshal(testdata.CustomElementByteSlice{Data: []testdata.MarshaledOctet{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(customBytePayload), `{"data":["octet","octet"]}`; got != want {
		t.Fatalf("encoding/json marshaled-byte wire payload = %s, want %s", got, want)
	}
	jsonPointer := testdata.AliasJSONPointer(1)
	textPointer := testdata.AliasTextPointer(1)
	aliasMarshalerPayload, err := json.Marshal(&testdata.AliasResultMarshalers{
		JSONValue:   testdata.AliasJSONValueOne,
		JSONPointer: jsonPointer,
		TextValue:   testdata.AliasTextValue(1),
		TextPointer: &textPointer,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantAliasMarshalerPayload = `{"json_value":"json-value","json_pointer":"json-pointer","text_value":"text-value","text_pointer":"text-pointer"}`
	if string(aliasMarshalerPayload) != wantAliasMarshalerPayload {
		t.Fatalf("encoding/json alias-result marshaler payload = %s, want %s", aliasMarshalerPayload, wantAliasMarshalerPayload)
	}
	unconstrainedPayload, err := json.Marshal(genericwire.UnconstrainedCustomMarshalerPayload{
		Box: genericwire.UnconstrainedWireBox[testdata.AliasJSONValue]{Value: testdata.AliasJSONValueOne},
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantUnconstrainedPayload = `{"box":{"value":"json-value"}}`
	if string(unconstrainedPayload) != wantUnconstrainedPayload {
		t.Fatalf("encoding/json unconstrained custom-marshaler payload = %s, want %s", unconstrainedPayload, wantUnconstrainedPayload)
	}

	runtime := loadTypeScriptFixture(t, "json-wire-parity.ts",
		"__DEPTH_JSON__", strconv.Quote(string(depthPayload)),
		"__NIL_DEPTH_JSON__", strconv.Quote(string(nilDepthPayload)),
		"__GENERIC_DEPTH_JSON__", strconv.Quote(string(genericDepthPayload)),
		"__GENERIC_NIL_DEPTH_JSON__", strconv.Quote(string(genericNilDepthPayload)),
		"__BYTE_JSON__", strconv.Quote(string(bytePayload)),
		"__CUSTOM_BYTE_JSON__", strconv.Quote(string(customBytePayload)),
		"__ALIAS_MARSHALER_JSON__", strconv.Quote(string(aliasMarshalerPayload)),
		"__UNCONSTRAINED_JSON__", strconv.Quote(string(unconstrainedPayload)),
	)
	writeContractSupportFiles(t, dir, runtime)
	runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")
	runContractCommand(t, dir, "bun", "run", "runtime.ts")
}

func TestGeneratedDependentGenericConstraintCompilesAndRuns(t *testing.T) {
	requireTypeScriptToolchain(t)
	schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
		Packages: []string{
			"tygor.dev/tygorgen/provider/testdata",
			"tygor.dev/tygorgen/provider/testdata/genericwire",
		},
		RootTypes: []provider.RootType{
			{Name: "DependentContainer"},
			{Package: "tygor.dev/tygorgen/provider/testdata/genericwire", Name: "ForwardedDependentWire"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	emitTypes := true
	generateContractFixture(t, dir, schema, GeneratorConfig{
		SingleFile: true,
		Custom: map[string]any{
			"Flavors":   []string{"zod", "zod-mini"},
			"EmitTypes": &emitTypes,
		},
	})

	runtime := loadTypeScriptFixture(t, "dependent-generic.ts")
	writeContractSupportFiles(t, dir, runtime)
	runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")
	runContractCommand(t, dir, "bun", "run", "runtime.ts")

	for _, name := range []string{"schemas.zod.ts", "schemas.zod-mini.ts"} {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "z.output<ESchema>") || !strings.Contains(string(content), "z.input<ESchema>") {
			t.Fatalf("%s omitted schema-derived dependent bounds:\n%s", name, content)
		}
	}
}

func TestGeneratedGenericScalarValidationReturnsClearError(t *testing.T) {
	for _, test := range []struct {
		root    string
		wantErr string
	}{
		{root: "GenericEmailBox", wantErr: "validator \"email\" on unresolved type parameter T is not supported"},
		{root: "GenericEmailPointerHolder", wantErr: "validator \"email\" on applied generic type GenericEmailPointer is not supported"},
	} {
		t.Run(test.root, func(t *testing.T) {
			schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
				Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
				RootTypes: []provider.RootType{{Name: test.root}},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, flavorName := range []string{"zod", "zod-mini"} {
				_, err = (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
					Sink: sink.NewMemorySink(),
					Config: GeneratorConfig{
						SingleFile: true,
						Custom:     map[string]any{"Flavors": []string{flavorName}},
					},
				})
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("flavor %s generation error = %v", flavorName, err)
				}
			}
		})
	}
}

func TestGeneratedRecursivePointerAliasReturnsClearError(t *testing.T) {
	schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: []provider.RootType{{Name: "RecursivePointerAliasHolder"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
		Sink: sink.NewMemorySink(),
		Config: GeneratorConfig{
			SingleFile: true,
			Custom:     map[string]any{"Flavors": []string{"zod"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "recursive pointer-only alias RecursivePointerAlias cannot be represented safely") {
		t.Fatalf("generation error = %v", err)
	}
}

func TestGeneratedRecursiveGenericPointerAliasReturnsClearError(t *testing.T) {
	schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: []provider.RootType{{Name: "RecursiveGenericPointerAliasHolder"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
		Sink: sink.NewMemorySink(),
		Config: GeneratorConfig{
			SingleFile: true,
			Custom:     map[string]any{"Flavors": []string{"zod", "zod-mini"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "recursive pointer-only alias RecursiveGenericPointerAlias cannot be represented safely") {
		t.Fatalf("generation error = %v", err)
	}
}

func TestGeneratedIndirectGenericPointerAliasReturnsClearError(t *testing.T) {
	for _, typeName := range []string{"IndirectRecursivePointerAlias", "NestedIndirectRecursivePointerAlias"} {
		schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
			Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
			RootTypes: []provider.RootType{{Name: typeName}},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, flavors := range [][]string{nil, {"zod"}, {"zod-mini"}} {
			_, err = (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
				Sink: sink.NewMemorySink(),
				Config: GeneratorConfig{
					SingleFile: true,
					Custom:     map[string]any{"Flavors": flavors},
				},
			})
			if err == nil || !strings.Contains(err.Error(), "recursive pointer-only alias "+typeName+" cannot be represented safely") {
				t.Fatalf("type %s flavors %v generation error = %v", typeName, flavors, err)
			}
		}
	}
}

func TestGeneratedFiniteAndProductiveGenericPointerAliasesCompile(t *testing.T) {
	requireTypeScriptToolchain(t)
	var nilPointer testdata.ProductivePointerMapAlias
	var nilMap map[string]testdata.ProductivePointerMapAlias
	pointerToNilMap := testdata.ProductivePointerMapAlias(&nilMap)
	emptyMap := map[string]testdata.ProductivePointerMapAlias{}
	pointerToEmptyMap := testdata.ProductivePointerMapAlias(&emptyMap)
	nestedMap := map[string]testdata.ProductivePointerMapAlias{"child": nil}
	pointerToNestedMap := testdata.ProductivePointerMapAlias(&nestedMap)
	mapPayloads := make([]string, 0, 4)
	for _, value := range []testdata.ProductivePointerMapAlias{
		nilPointer,
		pointerToNilMap,
		pointerToEmptyMap,
		pointerToNestedMap,
	} {
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		mapPayloads = append(mapPayloads, string(payload))
	}
	if got, want := strings.Join(mapPayloads, ","), `null,null,{},{"child":null}`; got != want {
		t.Fatalf("encoding/json pointer-map payloads = %s, want %s", got, want)
	}

	schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
		Packages: []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: []provider.RootType{
			{Name: "GenericPointerWrapper"},
			{Name: "RenamedGenericPointerWrapper"},
			{Name: "DeepGenericPointerWrapper"},
			{Name: "ProductiveIndirectPointerAlias"},
			{Name: "ProductivePointerMapAlias"},
			{Name: "ProductiveDoublePointerMapAlias"},
			{Name: "ProductiveGenericPointerMapAlias"},
			{Name: "ProductiveGenericHiddenMapAlias"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, emitTypes := range []bool{true, false} {
		emitTypes := emitTypes
		t.Run("emit_types_"+strconv.FormatBool(emitTypes), func(t *testing.T) {
			dir := t.TempDir()
			generateContractFixture(t, dir, schema, GeneratorConfig{
				SingleFile:      false,
				TrailingNewline: true,
				Custom: map[string]any{
					"Flavors":   []string{"zod", "zod-mini"},
					"EmitTypes": &emitTypes,
				},
			})
			runtime := loadTypeScriptFixture(t, "productive-pointer-aliases.ts",
				"__MAP_PAYLOADS__", strconv.Quote(strings.Join(mapPayloads, "\n"))+".split('\\n')",
			)
			writeContractSupportFiles(t, dir, runtime)
			runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")
			runContractCommand(t, dir, "bun", "run", "runtime.ts")
		})
	}
}

func TestGeneratedProductiveRecursiveAliasLengthValidationReturnsClearError(t *testing.T) {
	schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: []provider.RootType{{Name: "ProductiveValidatedPointerSliceAliasHolder"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, flavorName := range []string{"zod", "zod-mini"} {
		emitTypes := false
		_, err = (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
			Sink: sink.NewMemorySink(),
			Config: GeneratorConfig{
				SingleFile: true,
				Custom: map[string]any{
					"Flavors":   []string{flavorName},
					"EmitTypes": &emitTypes,
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "validator \"len\" on recursive alias ProductiveValidatedPointerSliceAlias is not supported") {
			t.Fatalf("flavor %s generation error = %v", flavorName, err)
		}
	}
}

func TestGeneratedGenericHiddenRecursiveMapAliasesReturnClearError(t *testing.T) {
	for _, typeName := range []string{"UnsupportedGenericHiddenMapAlias", "UnsupportedAppliedGenericMapAlias"} {
		schema, err := (&provider.SourceProvider{}).BuildSchema(context.Background(), provider.SourceInputOptions{
			Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
			RootTypes: []provider.RootType{{Name: typeName}},
		})
		if err != nil {
			t.Fatal(err)
		}
		configs := []GeneratorConfig{
			{SingleFile: true},
			{SingleFile: true, Custom: map[string]any{"Flavors": []string{"zod", "zod-mini"}}},
		}
		for _, flavorName := range []string{"zod", "zod-mini"} {
			emitTypes := false
			configs = append(configs, GeneratorConfig{
				SingleFile: true,
				Custom: map[string]any{
					"Flavors":   []string{flavorName},
					"EmitTypes": &emitTypes,
				},
			})
		}
		for _, config := range configs {
			_, err := (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
				Sink:   sink.NewMemorySink(),
				Config: config,
			})
			if err == nil || !strings.Contains(err.Error(), "recursive alias "+typeName+" cannot be represented safely in TypeScript: unguarded alias cycle") {
				t.Fatalf("type %s config %#v generation error = %v", typeName, config.Custom, err)
			}
			if strings.Contains(err.Error(), "pointer-only") {
				t.Fatalf("type %s received misleading pointer-only error: %v", typeName, err)
			}
		}
	}
}

func TestGeneratedReflectedProductivePointerMapAliasCompiles(t *testing.T) {
	requireTypeScriptToolchain(t)
	schema, err := (&provider.ReflectionProvider{}).BuildSchema(context.Background(), provider.ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeFor[testdata.ProductivePointerMapAlias]()},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	emitTypes := true
	generateContractFixture(t, dir, schema, GeneratorConfig{
		SingleFile:      false,
		TrailingNewline: true,
		Custom: map[string]any{
			"Flavors":   []string{"zod", "zod-mini"},
			"EmitTypes": &emitTypes,
		},
	})
	writeContractSupportFiles(t, dir, loadTypeScriptFixture(t, "reflected-pointer-map.ts"))
	runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")
	runContractCommand(t, dir, "bun", "run", "runtime.ts")
}

func TestGeneratedReflectedPointerAliasRootReturnsClearError(t *testing.T) {
	schema, err := (&provider.ReflectionProvider{}).BuildSchema(context.Background(), provider.ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeFor[*reflectedRecursivePointerAlias]()},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
		Sink: sink.NewMemorySink(),
		Config: GeneratorConfig{
			SingleFile: true,
			Custom:     map[string]any{"Flavors": []string{"zod", "zod-mini"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "recursive pointer-only alias reflectedRecursivePointerAlias cannot be represented safely") {
		t.Fatalf("generation error = %v", err)
	}
}

func TestGeneratedScalarOnlyManifestCompiles(t *testing.T) {
	requireTypeScriptToolchain(t)
	schema := &ir.Schema{Services: []ir.ServiceDescriptor{{Name: "Scalars", Endpoints: []ir.EndpointDescriptor{
		{Name: "Get", FullName: "Scalars.Get", Primitive: "query", Path: "/Scalars/Get", Request: ir.String(), Response: ir.Map(ir.String(), ir.Int(32))},
	}}}}
	dir := t.TempDir()
	generateContractFixture(t, dir, schema, GeneratorConfig{SingleFile: true})
	writeContractSupportFiles(t, dir, loadTypeScriptFixture(t, "scalar-manifest.ts"))
	runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")

	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), `from "./types"`) {
		t.Fatalf("scalar-only manifest imported an empty type module:\n%s", manifest)
	}
}

func TestGeneratedZodOnlyManifestCompiles(t *testing.T) {
	requireTypeScriptToolchain(t)
	id := ir.GoIdentifier{Package: "example.com/model", Name: "Node"}
	schema := &ir.Schema{
		Package: ir.PackageInfo{Path: id.Package, Name: "model"},
		Types: []ir.TypeDescriptor{&ir.StructDescriptor{Name: id, Fields: []ir.FieldDescriptor{
			{Name: "Next", JSONName: "next", Type: ir.Ptr(ir.Ref(id.Name, id.Package))},
		}}},
		Services: []ir.ServiceDescriptor{{Name: "Nodes", Endpoints: []ir.EndpointDescriptor{
			{Name: "Get", FullName: "Nodes.Get", Primitive: "query", Path: "/Nodes/Get", Response: ir.Ref(id.Name, id.Package)},
		}}},
	}

	dir := t.TempDir()
	emitTypes := false
	generateContractFixture(t, dir, schema, GeneratorConfig{SingleFile: true, Custom: map[string]any{
		"Flavors":   []string{"zod"},
		"EmitTypes": &emitTypes,
	}})
	writeContractSupportFiles(t, dir, loadTypeScriptFixture(t, "zod-only-manifest.ts"))
	runContractCommand(t, dir, typescriptCompiler(t), "-p", "tsconfig.json")
	runContractCommand(t, dir, "bun", "run", "runtime.ts")

	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `from "./schemas.zod"`) {
		t.Fatalf("manifest did not import the inference-bearing flavor:\n%s", manifest)
	}
}

func TestValidationParametersRejectSourceText(t *testing.T) {
	schema := &ir.Schema{Types: []ir.TypeDescriptor{&ir.StructDescriptor{
		Name:   ir.GoIdentifier{Name: "Unsafe", Package: "example.com/model"},
		Fields: []ir.FieldDescriptor{{Name: "Value", JSONName: "value", Type: ir.Int(32), ValidateTag: "gte=0);globalThis.injected=true;//"}},
	}}}
	s := sink.NewMemorySink()
	_, err := (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{Sink: s, Config: GeneratorConfig{
		SingleFile: true,
		Custom:     map[string]any{"Flavors": []string{"zod"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "invalid gte parameter") {
		t.Fatalf("expected invalid validation parameter error, got %v", err)
	}
}

func generateContractFixture(t *testing.T, dir string, schema *ir.Schema, config GeneratorConfig) {
	t.Helper()
	if errs := schema.Validate(); len(errs) > 0 {
		t.Fatalf("invalid fixture schema: %v", errs[0])
	}
	_, err := (&TypeScriptGenerator{}).Generate(context.Background(), schema, GenerateOptions{
		Sink:   sink.NewFilesystemSink(dir),
		Config: config,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeContractSupportFiles(t *testing.T, dir, runtime string) {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "node_modules"), filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	tsconfig := `{"compilerOptions":{"strict":true,"noEmit":true,"target":"ES2022","module":"ESNext","moduleResolution":"Bundler","skipLibCheck":true},"include":["*.ts"]}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.ts"), []byte(runtime), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadTypeScriptFixture(t *testing.T, name string, replacements ...string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if len(replacements)%2 != 0 {
		t.Fatalf("TypeScript fixture %s has an unmatched replacement", name)
	}
	return strings.NewReplacer(replacements...).Replace(string(content))
}

func requireTypeScriptToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bun"); err != nil {
		t.Fatal("bun is required for e2e tests")
	}
	if _, err := os.Stat(typescriptCompiler(t)); err != nil {
		t.Fatal("TypeScript compiler is required for e2e tests")
	}
}

func typescriptCompiler(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "node_modules", ".bin", "tsc"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func runContractCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}
