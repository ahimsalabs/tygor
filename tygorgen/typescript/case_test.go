package typescript

import (
	"testing"
)

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"simple", "simple"},
		{"Simple", "simple"},
		{"my_field", "myField"},
		{"MY_FIELD", "myField"},
		{"already_camel", "alreadyCamel"},
		{"UPPER_CASE", "upperCase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toCamelCase(tt.input)
			if got != tt.want {
				t.Errorf("toCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"simple", "Simple"},
		{"Simple", "Simple"},
		{"my_field", "MyField"},
		{"MY_FIELD", "MyField"},
		{"already_pascal", "AlreadyPascal"},
		{"UPPER_CASE", "UpperCase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toPascalCase(tt.input)
			if got != tt.want {
				t.Errorf("toPascalCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"simple", "simple"},
		{"Simple", "simple"},
		{"MyField", "my_field"},
		{"myField", "my_field"},
		{"AlreadySnake", "already_snake"},
		{"HTTPResponse", "h_t_t_p_response"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toSnakeCase(tt.input)
			if got != tt.want {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToKebabCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"simple", "simple"},
		{"Simple", "simple"},
		{"MyField", "my-field"},
		{"myField", "my-field"},
		{"AlreadyKebab", "already-kebab"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toKebabCase(tt.input)
			if got != tt.want {
				t.Errorf("toKebabCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestApplyCaseTransform(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		caseStyle string
		want      string
	}{
		{"preserve", "MyField", "preserve", "MyField"},
		{"preserve empty", "MyField", "", "MyField"},
		{"camel", "MyField", "camel", "myField"},
		{"pascal", "my_field", "pascal", "MyField"},
		{"snake", "MyField", "snake", "my_field"},
		{"kebab", "MyField", "kebab", "my-field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyCaseTransform(tt.input, tt.caseStyle)
			if got != tt.want {
				t.Errorf("applyCaseTransform(%q, %q) = %q, want %q", tt.input, tt.caseStyle, got, tt.want)
			}
		})
	}
}

func TestApplyNameTransforms(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		config GeneratorConfig
		want   string
	}{
		{
			name:  "no transforms",
			input: "User",
			config: GeneratorConfig{
				TypeCase: "preserve",
			},
			want: "User",
		},
		{
			name:  "prefix and suffix",
			input: "User",
			config: GeneratorConfig{
				TypePrefix: "API",
				TypeSuffix: "DTO",
			},
			want: "APIUserDTO",
		},
		{
			name:  "camel case",
			input: "UserType",
			config: GeneratorConfig{
				TypeCase: "camel",
			},
			want: "userType",
		},
		{
			name:  "snake case with prefix",
			input: "UserType",
			config: GeneratorConfig{
				TypePrefix: "api_",
				TypeCase:   "snake",
			},
			want: "api_user_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyNameTransforms(tt.input, tt.config)
			if got != tt.want {
				t.Errorf("applyNameTransforms(%q, config) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
