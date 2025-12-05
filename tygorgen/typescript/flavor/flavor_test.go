package flavor

import (
	"testing"
)

func TestParseValidateTag(t *testing.T) {
	tests := []struct {
		tag  string
		want []ValidateRule
	}{
		{"", nil},
		{"required", []ValidateRule{{Name: "required"}}},
		{"required,email", []ValidateRule{{Name: "required"}, {Name: "email"}}},
		{"min=3", []ValidateRule{{Name: "min", Param: "3"}}},
		{"required,min=3,max=100", []ValidateRule{
			{Name: "required"},
			{Name: "min", Param: "3"},
			{Name: "max", Param: "100"},
		}},
		{"oneof=a b c", []ValidateRule{{Name: "oneof", Param: "a b c"}}},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			got := ParseValidateTag(tt.tag)
			if len(got) != len(tt.want) {
				t.Errorf("ParseValidateTag(%q) = %d rules, want %d", tt.tag, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name || got[i].Param != tt.want[i].Param {
					t.Errorf("ParseValidateTag(%q)[%d] = %+v, want %+v", tt.tag, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestValidateRule_ZodMethod(t *testing.T) {
	tests := []struct {
		rule     ValidateRule
		isString bool
		want     string
	}{
		{ValidateRule{Name: "required"}, true, ".min(1)"},
		{ValidateRule{Name: "required"}, false, ""},
		{ValidateRule{Name: "email"}, true, ".email()"},
		{ValidateRule{Name: "url"}, true, ".url()"},
		{ValidateRule{Name: "uuid"}, true, ".uuid()"},
		{ValidateRule{Name: "min", Param: "3"}, true, ".min(3)"},
		{ValidateRule{Name: "max", Param: "100"}, true, ".max(100)"},
		{ValidateRule{Name: "len", Param: "10"}, true, ".length(10)"},
		{ValidateRule{Name: "gt", Param: "0"}, false, ".gt(0)"},
		{ValidateRule{Name: "gte", Param: "1"}, false, ".gte(1)"},
		{ValidateRule{Name: "lt", Param: "100"}, false, ".lt(100)"},
		{ValidateRule{Name: "lte", Param: "99"}, false, ".lte(99)"},
		{ValidateRule{Name: "alphanum"}, true, ".regex(/^[a-zA-Z0-9]+$/)"},
		{ValidateRule{Name: "datetime"}, true, ".datetime()"},
		{ValidateRule{Name: "ip"}, true, ".ip()"},
		{ValidateRule{Name: "ipv4"}, true, ".ip({ version: \"v4\" })"},
		{ValidateRule{Name: "base64"}, true, ".regex(/^[A-Za-z0-9+/]*={0,2}$/)"},
		{ValidateRule{Name: "json"}, true, ".refine((v) => { try { JSON.parse(v); return true; } catch { return false; } }, { message: 'Invalid JSON' })"},
		{ValidateRule{Name: "hostname"}, true, ".regex(/^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\\.)*[a-zA-Z]{2,}$/)"},
		{ValidateRule{Name: "mac"}, true, ".regex(/^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$/)"},
		{ValidateRule{Name: "semver"}, true, ".regex(/^\\d+\\.\\d+\\.\\d+(-[a-zA-Z0-9.]+)?(\\+[a-zA-Z0-9.]+)?$/)"},
		{ValidateRule{Name: "e164"}, true, ".regex(/^\\+[1-9]\\d{1,14}$/)"},
		{ValidateRule{Name: "omitempty"}, true, ""},
		{ValidateRule{Name: "dive"}, true, ""},
		{ValidateRule{Name: "unknown"}, true, ""},
	}

	for _, tt := range tests {
		name := tt.rule.Name
		if tt.rule.Param != "" {
			name += "=" + tt.rule.Param
		}
		t.Run(name, func(t *testing.T) {
			got := tt.rule.ZodMethod(tt.isString)
			if got != tt.want {
				t.Errorf("ZodMethod(%+v, %v) = %q, want %q", tt.rule, tt.isString, got, tt.want)
			}
		})
	}
}

func TestHasRequired(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"", false},
		{"email", false},
		{"required", true},
		{"required,email", true},
		{"email,required,min=3", true},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			rules := ParseValidateTag(tt.tag)
			if got := HasRequired(rules); got != tt.want {
				t.Errorf("HasRequired(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestHasOneOf(t *testing.T) {
	tests := []struct {
		tag  string
		want []string
	}{
		{"", nil},
		{"required", nil},
		{"oneof=a b c", []string{"a", "b", "c"}},
		{"required,oneof=draft published", []string{"draft", "published"}},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			rules := ParseValidateTag(tt.tag)
			got := HasOneOf(rules)
			if len(got) != len(tt.want) {
				t.Errorf("HasOneOf(%q) = %v, want %v", tt.tag, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("HasOneOf(%q)[%d] = %q, want %q", tt.tag, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"zod", false},
		{"zod-mini", false},
		{"unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Get(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
				return
			}
			if !tt.wantErr && f.Name() != tt.name {
				t.Errorf("Get(%q).Name() = %q, want %q", tt.name, f.Name(), tt.name)
			}
		})
	}
}
