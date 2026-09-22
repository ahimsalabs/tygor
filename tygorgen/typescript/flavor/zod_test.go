package flavor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"tygor.dev/tygorgen/ir"
)

func TestZodFlavor_EmitStruct(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "User"},
		Fields: []ir.FieldDescriptor{
			{Name: "ID", JSONName: "id", Type: ir.Int(64)},
			{Name: "Email", JSONName: "email", Type: ir.String(), ValidateTag: "required,email"},
			{Name: "Name", JSONName: "name", Type: ir.String(), OmitZero: true},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	// Check schema definition
	if !strings.Contains(output, "export const UserSchema = z.object({") {
		t.Error("missing schema definition")
	}

	// Check field with validation
	if !strings.Contains(output, `"email": z.string().min(1).email()`) {
		t.Errorf("missing email validation, got: %s", output)
	}

	// Check optional field
	if !strings.Contains(output, `"name": z.string().optional()`) {
		t.Errorf("missing optional field, got: %s", output)
	}

	// Should not have inferred type when EmitTypes is true
	if strings.Contains(output, "z.infer<typeof UserSchema>") {
		t.Error("should not emit inferred type when EmitTypes is true")
	}
}

func TestZodFlavor_EmitStruct_NoTypes(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: false, // No base types.ts
	}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "User"},
		Fields: []ir.FieldDescriptor{
			{Name: "Name", JSONName: "name", Type: ir.String()},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	// Should have inferred type when EmitTypes is false
	if !strings.Contains(output, "export type User = z.infer<typeof UserSchema>") {
		t.Errorf("missing inferred type, got: %s", output)
	}
}

func TestZodFlavor_EmitEnum(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	e := &ir.EnumDescriptor{
		Name: ir.GoIdentifier{Name: "Status"},
		Members: []ir.EnumMember{
			{Name: "Draft", Value: "draft"},
			{Name: "Published", Value: "published"},
			{Name: "Archived", Value: "archived"},
		},
	}

	got, err := f.EmitType(ctx, e)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	if !strings.Contains(output, `z.enum(["draft", "published", "archived"])`) {
		t.Errorf("missing enum values, got: %s", output)
	}
}

func TestZodFlavor_EmitStringifiedNumericEnum(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}
	e := &ir.EnumDescriptor{
		Name:                ir.GoIdentifier{Name: "Amount"},
		Members:             []ir.EnumMember{{Name: "Million", Value: float64(1e6)}},
		Underlying:          ir.Float(64),
		StringEncodedValues: []string{"1000000"},
	}

	got, err := f.EmitType(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if output := string(got); !strings.Contains(output, `z.enum(["1000000"])`) {
		t.Fatalf("stringified enum schema = %s", output)
	}
}

func TestZodFlavor_EmitAlias(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	a := &ir.AliasDescriptor{
		Name:       ir.GoIdentifier{Name: "UserID"},
		Underlying: ir.String(),
	}

	got, err := f.EmitType(ctx, a)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	if !strings.Contains(output, "export const UserIDSchema = z.string()") {
		t.Errorf("missing alias schema, got: %s", output)
	}
}

func TestZodFlavor_BitSizeConstraints(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Numbers"},
		Fields: []ir.FieldDescriptor{
			{Name: "Int8", JSONName: "int8", Type: ir.Int(8)},
			{Name: "Uint8", JSONName: "uint8", Type: ir.Uint(8)},
			{Name: "Int16", JSONName: "int16", Type: ir.Int(16)},
			{Name: "Int32", JSONName: "int32", Type: ir.Int(32)},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	if !strings.Contains(output, ".min(-128).max(127)") {
		t.Errorf("missing int8 constraints, got: %s", output)
	}
	if !strings.Contains(output, ".nonnegative().max(255)") {
		t.Errorf("missing uint8 constraints, got: %s", output)
	}
	if !strings.Contains(output, ".min(-32768).max(32767)") {
		t.Errorf("missing int16 constraints, got: %s", output)
	}
}

func TestZodFlavor_Preamble(t *testing.T) {
	tests := []struct {
		name string
		mini bool
		want string
	}{
		{"zod", false, `import { z } from 'zod';`},
		{"zod-mini", true, `import * as z from 'zod/mini';`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &ZodFlavor{mini: tt.mini}
			ctx := &EmitContext{}
			got := string(f.EmitPreamble(ctx))
			if !strings.Contains(got, tt.want) {
				t.Errorf("EmitPreamble() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestZodFlavor_FileExtension(t *testing.T) {
	tests := []struct {
		mini bool
		want string
	}{
		{false, ".zod.ts"},
		{true, ".zod-mini.ts"},
	}

	for _, tt := range tests {
		f := &ZodFlavor{mini: tt.mini}
		if got := f.FileExtension(); got != tt.want {
			t.Errorf("FileExtension() = %q, want %q", got, tt.want)
		}
	}
}

func TestZodFlavor_ComplexTypes(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Complex"},
		Fields: []ir.FieldDescriptor{
			{Name: "Tags", JSONName: "tags", Type: ir.Slice(ir.String())},
			{Name: "Metadata", JSONName: "metadata", Type: ir.Map(ir.String(), ir.Any())},
			{Name: "Nullable", JSONName: "nullable", Type: ir.Ptr(ir.String())},
			{Name: "Ref", JSONName: "ref", Type: ir.Ref("OtherType", "pkg")},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	if !strings.Contains(output, "z.array(z.string())") {
		t.Errorf("missing array type, got: %s", output)
	}
	if !strings.Contains(output, "z.record(z.string(), z.unknown())") {
		t.Errorf("missing map type, got: %s", output)
	}
	if !strings.Contains(output, "z.string().nullable()") {
		t.Errorf("missing nullable type, got: %s", output)
	}
	if !strings.Contains(output, "OtherTypeSchema") {
		t.Errorf("missing reference type, got: %s", output)
	}
}

func TestZodFlavor_OneOf(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "WithOneOf"},
		Fields: []ir.FieldDescriptor{
			{Name: "Status", JSONName: "status", Type: ir.String(), ValidateTag: "oneof=draft published archived"},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	if !strings.Contains(output, `.refine(v => ["draft", "published", "archived"].includes(v))`) {
		t.Errorf("missing oneof predicate, got: %s", output)
	}
}

func TestZodFlavor_UnsupportedValidatorWarning(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "WithUnsupported"},
		Fields: []ir.FieldDescriptor{
			{Name: "Field", JSONName: "field", Type: ir.String(), ValidateTag: "required,unknown_validator,email"},
		},
	}

	_, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	// Should have warning for unsupported validator
	if len(ctx.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(ctx.Warnings), ctx.Warnings)
	}
	if len(ctx.Warnings) > 0 && !strings.Contains(ctx.Warnings[0], "unknown_validator") {
		t.Errorf("warning should mention unknown_validator, got: %s", ctx.Warnings[0])
	}
}

func TestZodFlavor_SkippedValidatorNoWarning(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	// Validators like dive, omitempty, eqfield should be skipped without warning
	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "WithSkipped"},
		Fields: []ir.FieldDescriptor{
			{Name: "Field", JSONName: "field", Type: ir.String(), ValidateTag: "required,omitempty,dive,eqfield=Other"},
		},
	}

	_, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	// Should have no warnings for intentionally skipped validators
	if len(ctx.Warnings) != 0 {
		t.Errorf("expected 0 warnings for skipped validators, got %d: %v", len(ctx.Warnings), ctx.Warnings)
	}
}

func TestZodFlavor_RequiredDiveRequired(t *testing.T) {
	model := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "RequiredNames"},
		Fields: []ir.FieldDescriptor{{
			Name: "Names", JSONName: "names", Type: ir.Slice(ir.String()), ValidateTag: "required,dive,required",
		}},
	}
	for _, mini := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "mini"}[mini], func(t *testing.T) {
			output, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{IndentStr: "  ", EmitTypes: true}, model)
			if err != nil {
				t.Fatal(err)
			}
			got := string(output)
			want := "z.array(z.string().min(1))"
			if mini {
				want = "z.array(z.string().check(z.minLength(1)))"
			}
			if !strings.Contains(got, want) || strings.Contains(got, want+".nullable()") || strings.Contains(got, "z.nullable("+want+")") {
				t.Fatalf("required,dive,required schema = %s", got)
			}
		})
	}
}

func TestZodFlavor_RequiredPointerStringRequiresPresenceNotContent(t *testing.T) {
	model := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "RequiredPointer"},
		Fields: []ir.FieldDescriptor{{
			Name: "Value", JSONName: "value", Type: ir.Ptr(ir.String()), ValidateTag: "required",
		}},
	}
	for _, mini := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "mini"}[mini], func(t *testing.T) {
			output, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{IndentStr: "  ", EmitTypes: true}, model)
			if err != nil {
				t.Fatal(err)
			}
			got := string(output)
			if !strings.Contains(got, `"value": z.string()`) || strings.Contains(got, ".min(1)") || strings.Contains(got, "nullable") {
				t.Fatalf("required pointer string schema = %s", got)
			}
		})
	}
}

func TestZodFlavor_RejectsTimeEqualityParameters(t *testing.T) {
	for _, rule := range []string{"eq=');globalThis.injected=true;//", "ne=2026-01-01"} {
		for _, mini := range []bool{false, true} {
			model := &ir.StructDescriptor{
				Name:   ir.GoIdentifier{Name: "Timed"},
				Fields: []ir.FieldDescriptor{{Name: "At", JSONName: "at", Type: ir.Time(), ValidateTag: rule}},
			}
			_, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{IndentStr: "  ", EmitTypes: true}, model)
			if err == nil || !strings.Contains(err.Error(), "on time.Time is not supported") {
				t.Fatalf("%s mini=%v error = %v", rule, mini, err)
			}
		}
	}
}

func TestZodFlavor_NumericLenUsesEquality(t *testing.T) {
	model := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "WithExactNumber"},
		Fields: []ir.FieldDescriptor{{
			Name: "Value", JSONName: "value", Type: ir.Int(32), ValidateTag: "len=5",
		}},
	}

	for _, mini := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "mini"}[mini], func(t *testing.T) {
			got, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{IndentStr: "  ", EmitTypes: true}, model)
			if err != nil {
				t.Fatal(err)
			}
			output := string(got)
			if !strings.Contains(output, "v => v === 5") || strings.Contains(output, ".length(5)") || strings.Contains(output, "z.length(5)") {
				t.Fatalf("numeric len did not emit equality validation: %s", output)
			}
		})
	}
}

func TestZodFlavor_RejectsByteLengthValidation(t *testing.T) {
	goValue := struct {
		Data []byte `json:"data" validate:"len=2"`
	}{Data: []byte{1, 2}}
	if err := validator.New().Struct(goValue); err != nil {
		t.Fatalf("Go validator rejected two decoded bytes: %v", err)
	}
	wire, err := json.Marshal(goValue)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"data":"AQI="}` {
		t.Fatalf("Go JSON wire = %s, want base64 whose encoded length differs from decoded length", wire)
	}

	for _, rule := range []string{"min=2", "max=2", "len=2", "gt=2", "gte=2", "lt=2", "lte=2", "eq=2", "ne=2"} {
		for _, mini := range []bool{false, true} {
			t.Run(rule+map[bool]string{false: "/regular", true: "/mini"}[mini], func(t *testing.T) {
				model := &ir.StructDescriptor{
					Name: ir.GoIdentifier{Name: "WithBytes"},
					Fields: []ir.FieldDescriptor{{
						Name: "Data", JSONName: "data", Type: ir.Bytes(), ValidateTag: rule,
					}},
				}
				_, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{IndentStr: "  ", EmitTypes: true}, model)
				if err == nil || !strings.Contains(err.Error(), `on byte slices is not supported because JSON uses base64 encoding`) {
					t.Fatalf("byte length validator error = %v", err)
				}
			})
		}
	}
}

func TestZodFlavor_RejectsTypeDependentValidatorsOnUnknownWireSchema(t *testing.T) {
	aliasID := ir.GoIdentifier{Name: "CustomWire", Package: "example.com/model"}
	schema := &ir.Schema{Types: []ir.TypeDescriptor{
		&ir.AliasDescriptor{Name: aliasID, Underlying: ir.Any()},
	}}
	types := map[string]ir.TypeDescriptor{
		"direct":  ir.Any(),
		"pointer": ir.Ptr(ir.Any()),
		"alias":   ir.Ref(aliasID.Name, aliasID.Package),
	}
	for _, validator := range []string{"email", "url", "uuid", "uri"} {
		for typeName, typ := range types {
			for _, mini := range []bool{false, true} {
				name := validator + "/" + typeName + map[bool]string{false: "/regular", true: "/mini"}[mini]
				t.Run(name, func(t *testing.T) {
					model := &ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "CustomMarshalerValidation"},
						Fields: []ir.FieldDescriptor{{
							Name: "Address", JSONName: "address", Type: typ, ValidateTag: validator,
						}},
					}
					_, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{Schema: schema, IndentStr: "  ", EmitTypes: true}, model)
					if err == nil || !strings.Contains(err.Error(), `validator "`+validator+`"`) || !strings.Contains(err.Error(), "unknown wire schema") {
						t.Fatalf("unknown-wire validator error = %v", err)
					}
				})
			}
		}
	}
}

func TestZodFlavor_EmitsNestedValidatorComposition(t *testing.T) {
	model := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "WithNestedValidation"},
		Fields: []ir.FieldDescriptor{{
			Name: "Emails", JSONName: "emails", Type: ir.Slice(ir.String()), ValidateTag: "dive,email",
		}},
	}

	for _, mini := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "mini"}[mini], func(t *testing.T) {
			output, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{IndentStr: "  ", EmitTypes: true}, model)
			if err != nil {
				t.Fatal(err)
			}
			want := "z.array(z.string().email())"
			if mini {
				want = "z.array(z.string().check(z.email()))"
			}
			if got := string(output); !strings.Contains(got, want) {
				t.Fatalf("nested email validator schema = %s", got)
			}
		})
	}
}

func TestZodFlavor_RecursivePointerAliasValidationResolutionTerminates(t *testing.T) {
	pointerID := ir.GoIdentifier{Name: "P", Package: "example.com/model"}
	alias := &ir.AliasDescriptor{Name: pointerID, Underlying: ir.Ptr(ir.Ref(pointerID.Name, pointerID.Package))}
	schema := &ir.Schema{Types: []ir.TypeDescriptor{alias}}
	holder := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Holder", Package: pointerID.Package},
		Fields: []ir.FieldDescriptor{{
			Name: "Value", JSONName: "value", Type: ir.Ref(pointerID.Name, pointerID.Package),
		}},
	}

	got, err := (&ZodFlavor{}).EmitType(&EmitContext{
		Schema:         schema,
		IndentStr:      "  ",
		EmitTypes:      true,
		RecursiveTypes: map[ir.GoIdentifier]bool{pointerID: true},
	}, holder)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "z.lazy") {
		t.Fatalf("recursive holder did not preserve lazy reference: %s", got)
	}
}

func TestZodFlavor_RejectsAppliedGenericPointerOnlyAlias(t *testing.T) {
	id := ir.GoIdentifier{Name: "P", Package: "example.com/p"}
	alias := &ir.AliasDescriptor{
		Name:           id,
		TypeParameters: []ir.TypeParameterDescriptor{{ParamName: "T"}},
		Underlying: ir.Ptr(ir.RefWithArgs(
			id.Name,
			id.Package,
			&ir.TypeParameterDescriptor{ParamName: "T"},
		)),
	}
	schema := &ir.Schema{Types: []ir.TypeDescriptor{alias}}

	for _, mini := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "mini"}[mini], func(t *testing.T) {
			_, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{
				IndentStr: "  ", EmitTypes: true, Schema: schema,
			}, alias)
			if err == nil || !strings.Contains(err.Error(), "recursive pointer-only alias P cannot be represented safely") {
				t.Fatalf("applied generic pointer alias error = %v", err)
			}
		})
	}
}

func TestZodFlavor_AllPrimitiveTypes(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{
		IndentStr: "  ",
		EmitTypes: true,
	}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "AllPrimitives"},
		Fields: []ir.FieldDescriptor{
			{Name: "Bool", JSONName: "bool", Type: ir.Bool()},
			{Name: "String", JSONName: "string", Type: ir.String()},
			{Name: "Int", JSONName: "int", Type: ir.Int(0)},
			{Name: "Int64", JSONName: "int64", Type: ir.Int(64)},
			{Name: "Uint", JSONName: "uint", Type: ir.Uint(0)},
			{Name: "Uint16", JSONName: "uint16", Type: ir.Uint(16)},
			{Name: "Uint32", JSONName: "uint32", Type: ir.Uint(32)},
			{Name: "Uint64", JSONName: "uint64", Type: ir.Uint(64)},
			{Name: "Float32", JSONName: "float32", Type: ir.Float(32)},
			{Name: "Float64", JSONName: "float64", Type: ir.Float(64)},
			{Name: "Bytes", JSONName: "bytes", Type: ir.Bytes()},
			{Name: "Time", JSONName: "time", Type: ir.Time()},
			{Name: "Duration", JSONName: "duration", Type: ir.Duration()},
			{Name: "Any", JSONName: "any", Type: ir.Any()},
			{Name: "Empty", JSONName: "empty", Type: ir.Empty()},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)

	checks := []string{
		"z.boolean()",
		"z.string()",
		"z.number().int()",
		"z.number().int().nonnegative()",
		"z.number()",
		"z.string().datetime()",
		"z.unknown()",
		"z.object({}).strict()",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("missing %q in output: %s", check, output)
		}
	}
}

func TestZodFlavor_MoreValidators(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"contains=foo", `.includes("foo")`},
		{"startswith=pre", `.startsWith("pre")`},
		{"endswith=suf", `.endsWith("suf")`},
		{"eq=val", `.refine(v => v === "val")`},
		{"ne=bad", `.refine(v => v !== "bad")`},
		{"alpha", `.regex(/^[a-zA-Z]+$/)`},
		{"numeric", `.regex(/^[0-9]+$/)`},
		{"lowercase", `.regex(/^[a-z]+$/)`},
		{"uppercase", `.regex(/^[A-Z]+$/)`},
	}

	f := &ZodFlavor{}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}
			s := &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Test"},
				Fields: []ir.FieldDescriptor{
					{Name: "F", JSONName: "f", Type: ir.String(), ValidateTag: tt.tag},
				},
			}
			got, err := f.EmitType(ctx, s)
			if err != nil {
				t.Fatalf("EmitType error: %v", err)
			}
			if !strings.Contains(string(got), tt.want) {
				t.Errorf("expected %q in output, got: %s", tt.want, got)
			}
		})
	}
}

func TestZodFlavor_NumericValidators(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Numeric"},
		Fields: []ir.FieldDescriptor{
			{Name: "Age", JSONName: "age", Type: ir.Int(0), ValidateTag: "gt=0,lte=150"},
			{Name: "EqNum", JSONName: "eq_num", Type: ir.Int(0), ValidateTag: "eq=42"},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	if !strings.Contains(output, ".gt(0)") {
		t.Errorf("missing .gt(0): %s", output)
	}
	if !strings.Contains(output, ".lte(150)") {
		t.Errorf("missing .lte(150): %s", output)
	}
	if !strings.Contains(output, ".refine(v => v === 42)") {
		t.Errorf("missing eq refine: %s", output)
	}
}

func TestZodFlavor_UnionType(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "WithUnion"},
		Fields: []ir.FieldDescriptor{
			{Name: "Value", JSONName: "value", Type: ir.Union(ir.String(), ir.Int(0))},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	if !strings.Contains(string(got), "z.union([z.string(), z.number().int()])") {
		t.Errorf("missing union type: %s", got)
	}
}

func TestZodFlavor_TypeParameter(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name:           ir.GoIdentifier{Name: "Generic"},
		TypeParameters: []ir.TypeParameterDescriptor{*ir.TypeParam("T", nil)},
		Fields: []ir.FieldDescriptor{
			{Name: "Data", JSONName: "data", Type: ir.TypeParam("T", nil)},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	if !strings.Contains(string(got), "GenericSchema<TSchema extends z.ZodType>(TSchema: TSchema)") ||
		!strings.Contains(string(got), `"data": TSchema`) {
		t.Errorf("expected parameterized schema for type param: %s", got)
	}
}

func TestZodFlavor_NumericEnum(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	e := &ir.EnumDescriptor{
		Name: ir.GoIdentifier{Name: "Priority"},
		Members: []ir.EnumMember{
			{Name: "Low", Value: int64(1)},
			{Name: "Medium", Value: int64(2)},
			{Name: "High", Value: int64(3)},
		},
	}

	got, err := f.EmitType(ctx, e)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	// Numeric enums use z.union of literals
	if !strings.Contains(output, "z.literal(1)") {
		t.Errorf("missing z.literal(1): %s", output)
	}
	if !strings.Contains(output, "z.union([") {
		t.Errorf("missing z.union: %s", output)
	}
}

func TestZodFlavor_EmptyEnum(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	e := &ir.EnumDescriptor{
		Name:    ir.GoIdentifier{Name: "Empty"},
		Members: []ir.EnumMember{},
	}

	got, err := f.EmitType(ctx, e)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	if !strings.Contains(string(got), "z.never()") {
		t.Errorf("expected z.never() for empty enum: %s", got)
	}
}

func TestZodFlavor_StringEncoded(t *testing.T) {
	f := &ZodFlavor{}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Test"},
		Fields: []ir.FieldDescriptor{
			{Name: "BigID", JSONName: "big_id", Type: ir.Int(64), StringEncoded: true},
			{Name: "Count", JSONName: "count", Type: ir.Int(32), StringEncoded: false},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// String-encoded fields retain their JSON wire values rather than coercing.
	if !strings.Contains(output, `"big_id": z.string().regex(`) || !strings.Contains(output, "BigInt(v)") {
		t.Errorf("expected bounded wire string for StringEncoded int64, got: %s", output)
	}
	if !strings.Contains(output, `json:",string"`) {
		t.Errorf("expected json:\",string\" in comment, got: %s", output)
	}

	// Count should use regular z.number()
	if !strings.Contains(output, `"count": z.number().int()`) {
		t.Errorf("expected regular z.number().int() for non-StringEncoded, got: %s", output)
	}

}

func TestZodFlavor_StringEncodedNamedNumericEnum(t *testing.T) {
	pkg := "example.com/model"
	enumID := ir.GoIdentifier{Name: "Level", Package: pkg}
	schema := &ir.Schema{Types: []ir.TypeDescriptor{
		&ir.EnumDescriptor{Name: enumID, Members: []ir.EnumMember{{Name: "Low", Value: int64(1)}, {Name: "High", Value: int64(2)}}},
	}}
	model := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Named", Package: pkg},
		Fields: []ir.FieldDescriptor{
			{Name: "Level", JSONName: "level", Type: ir.Ref(enumID.Name, enumID.Package), StringEncoded: true},
		},
	}

	for _, mini := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "mini"}[mini], func(t *testing.T) {
			got, err := (&ZodFlavor{mini: mini}).EmitType(&EmitContext{Schema: schema, IndentStr: "  ", EmitTypes: true}, model)
			if err != nil {
				t.Fatal(err)
			}
			output := string(got)
			if !strings.Contains(output, `BigInt(v).toString()`) {
				t.Fatalf("numeric enum did not validate encoded wire members: %s", output)
			}
		})
	}
}

func TestZodFlavor_StringEncodedAppliedDefinedPointer(t *testing.T) {
	pkg := "example.com/model"
	pointerID := ir.GoIdentifier{Name: "PhantomPointer", Package: pkg}
	applied := ir.RefWithArgs(pointerID.Name, pointerID.Package, ir.String())
	schema := &ir.Schema{Types: []ir.TypeDescriptor{&ir.AliasDescriptor{
		Name:           pointerID,
		TypeParameters: []ir.TypeParameterDescriptor{{ParamName: "T"}},
		Underlying:     ir.Ptr(ir.Int(64)),
	}}}

	for _, mini := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "mini"}[mini], func(t *testing.T) {
			flavor := &ZodFlavor{mini: mini}
			ctx := &EmitContext{Schema: schema}
			encoded, err := flavor.typeToZod(ctx, applied, true)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(encoded, "z.string()") || !strings.Contains(encoded, "nullable") || strings.Contains(encoded, "PhantomPointerSchema") {
				t.Fatalf("string-encoded applied pointer schema = %s", encoded)
			}

			plain, err := flavor.typeToZod(ctx, applied, false)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(plain, "PhantomPointerSchema") {
				t.Fatalf("ordinary applied pointer bypassed its named schema: %s", plain)
			}

			double, err := flavor.typeToZod(ctx, ir.Ptr(applied), false)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(double, "PhantomPointerSchema") || !strings.Contains(double, "nullable") {
				t.Fatalf("double pointer schema = %s", double)
			}
		})
	}
}

// ============================================================================
// Zod-Mini Tests
// ============================================================================

func TestZodMiniFlavor_EmitStruct(t *testing.T) {
	f := &ZodFlavor{mini: true}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "User"},
		Fields: []ir.FieldDescriptor{
			{Name: "ID", JSONName: "id", Type: ir.Int(64)},
			{Name: "Email", JSONName: "email", Type: ir.String(), ValidateTag: "required,email"},
			{Name: "Name", JSONName: "name", Type: ir.String(), OmitZero: true},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// Should use z.number() with .check() for int constraints
	if !strings.Contains(output, "z.number().check(") {
		t.Errorf("expected z.number().check() for int64, got: %s", output)
	}

	// Should use z.optional() wrapper for optional fields
	if !strings.Contains(output, "z.optional(z.string())") {
		t.Errorf("expected z.optional(z.string()) for optional field, got: %s", output)
	}

	// Should use .check() for validations
	if !strings.Contains(output, ".check(z.minLength(1), z.email())") {
		t.Errorf("expected .check(z.minLength(1), z.email()) for email validation, got: %s", output)
	}
}

func TestZodMiniFlavor_NullableField(t *testing.T) {
	f := &ZodFlavor{mini: true}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Test"},
		Fields: []ir.FieldDescriptor{
			{Name: "Ptr", JSONName: "ptr", Type: ir.Ptr(ir.String())},
			{Name: "OptPtr", JSONName: "opt_ptr", Type: ir.Ptr(ir.Int(32)), OmitZero: true},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// Should use z.nullable() wrapper
	if !strings.Contains(output, "z.nullable(z.string())") {
		t.Errorf("expected z.nullable(z.string()) for pointer, got: %s", output)
	}

	// omitzero removes the nil pointer state, so a present value is non-null.
	if !strings.Contains(output, "z.optional(z.number()") || strings.Contains(output, "z.optional(z.nullable(") {
		t.Errorf("expected non-null optional pointer schema, got: %s", output)
	}
}

func TestZodMiniFlavor_Primitives(t *testing.T) {
	f := &ZodFlavor{mini: true}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Primitives"},
		Fields: []ir.FieldDescriptor{
			{Name: "Bool", JSONName: "bool", Type: ir.Bool()},
			{Name: "String", JSONName: "string", Type: ir.String()},
			{Name: "Int32", JSONName: "int32", Type: ir.Int(32)},
			{Name: "Uint8", JSONName: "uint8", Type: ir.Uint(8)},
			{Name: "Float64", JSONName: "float64", Type: ir.Float(64)},
			{Name: "Time", JSONName: "time", Type: ir.Time()},
			{Name: "Duration", JSONName: "duration", Type: ir.Duration()},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// Check primitives use correct zod-mini syntax
	checks := []struct {
		field    string
		expected string
	}{
		{"bool", "z.boolean()"},
		{"string", "z.string()"},
		{"int32", "z.number().check(z.int(), z.gte(-2147483648), z.lte(2147483647))"},
		{"uint8", "z.number().check(z.int(), z.gte(0), z.lte(255))"},
		{"float64", "z.number()"},
		{"time", "z.string().check(z.iso.datetime())"},
		{"duration", "z.number().check(z.int())"},
	}

	for _, c := range checks {
		if !strings.Contains(output, `"`+c.field+`": `+c.expected) {
			t.Errorf("expected %s: %s, got: %s", c.field, c.expected, output)
		}
	}
}

func TestZodMiniFlavor_Validations(t *testing.T) {
	f := &ZodFlavor{mini: true}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Validated"},
		Fields: []ir.FieldDescriptor{
			{Name: "Username", JSONName: "username", Type: ir.String(), ValidateTag: "required,min=3,max=20"},
			{Name: "Age", JSONName: "age", Type: ir.Int(32), ValidateTag: "gte=0,lte=150"},
			{Name: "URL", JSONName: "url", Type: ir.String(), ValidateTag: "url"},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// String validations should use z.minLength, z.maxLength
	if !strings.Contains(output, "z.minLength(1)") {
		t.Errorf("expected z.minLength(1) for required, got: %s", output)
	}
	if !strings.Contains(output, "z.minLength(3)") {
		t.Errorf("expected z.minLength(3) for min=3, got: %s", output)
	}
	if !strings.Contains(output, "z.maxLength(20)") {
		t.Errorf("expected z.maxLength(20) for max=20, got: %s", output)
	}

	// Numeric validations should use z.gte, z.lte
	if !strings.Contains(output, "z.gte(0)") {
		t.Errorf("expected z.gte(0), got: %s", output)
	}
	if !strings.Contains(output, "z.lte(150)") {
		t.Errorf("expected z.lte(150), got: %s", output)
	}

	// URL should use z.url()
	if !strings.Contains(output, "z.url()") {
		t.Errorf("expected z.url() for url validation, got: %s", output)
	}
}

func TestZodMiniFlavor_OneOf(t *testing.T) {
	f := &ZodFlavor{mini: true}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Priority"},
		Fields: []ir.FieldDescriptor{
			{Name: "Level", JSONName: "level", Type: ir.String(), ValidateTag: "oneof=low medium high"},
			{Name: "OptLevel", JSONName: "opt_level", Type: ir.String(), ValidateTag: "oneof=a b c", OmitZero: true},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	if !strings.Contains(output, `z.refine(v => ["low", "medium", "high"].includes(v))`) {
		t.Errorf("expected oneof predicate, got: %s", output)
	}

	// Optional oneof should wrap with z.optional()
	if !strings.Contains(output, `z.optional(z.string().check(z.refine(v => ["a", "b", "c"].includes(v))))`) {
		t.Errorf("expected optional oneof predicate, got: %s", output)
	}
}

func TestZodMiniFlavor_Arrays(t *testing.T) {
	f := &ZodFlavor{mini: true}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Lists"},
		Fields: []ir.FieldDescriptor{
			{Name: "Tags", JSONName: "tags", Type: ir.Slice(ir.String())},
			{Name: "Numbers", JSONName: "numbers", Type: ir.Slice(ir.Int(32)), ValidateTag: "max=10"},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// Should use z.array()
	if !strings.Contains(output, "z.array(z.string())") {
		t.Errorf("expected z.array(z.string()), got: %s", output)
	}

	// Array with validation
	if !strings.Contains(output, "z.array(") && strings.Contains(output, ".check(z.maxLength(10))") {
		t.Errorf("expected array with maxLength check, got: %s", output)
	}
}
