package typescript

import (
	"testing"
)

func TestEscapeReservedWord(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"interface", "interface_"},
		{"class", "class_"},
		{"type", "type_"},
		{"export", "export_"},
		{"default", "default_"},
		{"MyType", "MyType"},
		{"userName", "userName"},
		{"_private", "_private"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeReservedWord(tt.input)
			if got != tt.want {
				t.Errorf("escapeReservedWord(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNeedsQuoting(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"123abc", true},
		{"my-field", true},
		{"my.field", true},
		{"my field", true},
		{"interface", true},
		{"myField", false},
		{"_field", false},
		{"$field", false},
		{"field123", false},
		{"MyType", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := needsQuoting(tt.input)
			if got != tt.want {
				t.Errorf("needsQuoting(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "_"},
		{"123abc", "_123abc"},
		{"my-field", "my_field"},
		{"my.field", "my_field"},
		{"my field", "my_field"},
		{"interface", "interface_"},
		{"validName", "validName"},
		{"_underscore", "_underscore"},
		{"$dollar", "$dollar"},
		{"café", "café"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeIdentifier(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
