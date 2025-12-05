package flavor

import (
	"strings"
	"testing"

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
			{Name: "Name", JSONName: "name", Type: ir.String(), Optional: true},
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
	if !strings.Contains(output, `email: z.string().min(1).email()`) {
		t.Errorf("missing email validation, got: %s", output)
	}

	// Check optional field
	if !strings.Contains(output, `name: z.string().optional()`) {
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

	if !strings.Contains(output, `z.enum(["draft", "published", "archived"])`) {
		t.Errorf("missing oneof enum, got: %s", output)
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
		Name: ir.GoIdentifier{Name: "Generic"},
		Fields: []ir.FieldDescriptor{
			{Name: "Data", JSONName: "data", Type: ir.TypeParam("T", nil)},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	// Generic type params become z.unknown()
	if !strings.Contains(string(got), "z.unknown()") {
		t.Errorf("expected z.unknown() for type param: %s", got)
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
			{Name: "Flag", JSONName: "flag", Type: ir.Bool(), StringEncoded: true},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// BigID should use z.coerce.number() with json:",string" comment
	if !strings.Contains(output, "z.coerce.number()") {
		t.Errorf("expected z.coerce.number() for StringEncoded int64, got: %s", output)
	}
	if !strings.Contains(output, `json:",string"`) {
		t.Errorf("expected json:\",string\" in comment, got: %s", output)
	}

	// Count should use regular z.number()
	if !strings.Contains(output, "count: z.number().int()") {
		t.Errorf("expected regular z.number().int() for non-StringEncoded, got: %s", output)
	}

	// Flag should use z.coerce.boolean()
	if !strings.Contains(output, "z.coerce.boolean()") {
		t.Errorf("expected z.coerce.boolean() for StringEncoded bool, got: %s", output)
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
			{Name: "Name", JSONName: "name", Type: ir.String(), Optional: true},
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
			{Name: "OptPtr", JSONName: "opt_ptr", Type: ir.Ptr(ir.Int(32)), Optional: true},
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

	// Should wrap nullable in optional: z.optional(z.nullable(...))
	if !strings.Contains(output, "z.optional(z.nullable(") {
		t.Errorf("expected z.optional(z.nullable(...)) for optional pointer, got: %s", output)
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
		if !strings.Contains(output, c.field+": "+c.expected) {
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
			{Name: "OptLevel", JSONName: "opt_level", Type: ir.String(), ValidateTag: "oneof=a b c", Optional: true},
		},
	}

	got, err := f.EmitType(ctx, s)
	if err != nil {
		t.Fatalf("EmitType error: %v", err)
	}

	output := string(got)
	t.Log(output)

	// Should use z.enum for oneof
	if !strings.Contains(output, `z.enum(["low", "medium", "high"])`) {
		t.Errorf("expected z.enum for oneof, got: %s", output)
	}

	// Optional oneof should wrap with z.optional()
	if !strings.Contains(output, `z.optional(z.enum(["a", "b", "c"]))`) {
		t.Errorf("expected z.optional(z.enum(...)) for optional oneof, got: %s", output)
	}
}

func TestZodMiniFlavor_Arrays(t *testing.T) {
	f := &ZodFlavor{mini: true}
	ctx := &EmitContext{IndentStr: "  ", EmitTypes: true}

	s := &ir.StructDescriptor{
		Name: ir.GoIdentifier{Name: "Lists"},
		Fields: []ir.FieldDescriptor{
			{Name: "Tags", JSONName: "tags", Type: ir.Array(ir.String(), 0)},
			{Name: "Numbers", JSONName: "numbers", Type: ir.Array(ir.Int(32), 0), ValidateTag: "max=10"},
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
