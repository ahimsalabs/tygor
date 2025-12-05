package ir

import "testing"

func TestGoIdentifier_IsZero(t *testing.T) {
	tests := []struct {
		name string
		id   GoIdentifier
		want bool
	}{
		{"empty", GoIdentifier{}, true},
		{"name only", GoIdentifier{Name: "Foo"}, false},
		{"package only", GoIdentifier{Package: "pkg"}, false},
		{"both", GoIdentifier{Name: "Foo", Package: "pkg"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.IsZero(); got != tt.want {
				t.Errorf("GoIdentifier.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDocumentation_IsZero(t *testing.T) {
	deprecatedMsg := "use NewFoo"
	tests := []struct {
		name string
		doc  Documentation
		want bool
	}{
		{"empty", Documentation{}, true},
		{"summary only", Documentation{Summary: "A summary"}, false},
		{"body only", Documentation{Body: "Full body"}, false},
		{"deprecated only", Documentation{Deprecated: &deprecatedMsg}, false},
		{"all fields", Documentation{Summary: "s", Body: "b", Deprecated: &deprecatedMsg}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.doc.IsZero(); got != tt.want {
				t.Errorf("Documentation.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSource_IsZero(t *testing.T) {
	tests := []struct {
		name string
		src  Source
		want bool
	}{
		{"empty", Source{}, true},
		{"file only", Source{File: "foo.go"}, false},
		{"line only", Source{Line: 10}, false},
		{"column only", Source{Column: 5}, false},
		{"all fields", Source{File: "foo.go", Line: 10, Column: 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.src.IsZero(); got != tt.want {
				t.Errorf("Source.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageInfo_IsZero(t *testing.T) {
	tests := []struct {
		name string
		pkg  PackageInfo
		want bool
	}{
		{"empty", PackageInfo{}, true},
		{"path only", PackageInfo{Path: "github.com/foo/bar"}, false},
		{"name only", PackageInfo{Name: "bar"}, false},
		{"dir only", PackageInfo{Dir: "/home/user/bar"}, false},
		{"all fields", PackageInfo{Path: "p", Name: "n", Dir: "d"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pkg.IsZero(); got != tt.want {
				t.Errorf("PackageInfo.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWarning(t *testing.T) {
	src := &Source{File: "test.go", Line: 10}
	w := Warning{
		Code:     "W001",
		Message:  "something happened",
		Source:   src,
		TypeName: "MyType",
	}

	if w.Code != "W001" {
		t.Errorf("Warning.Code = %q, want W001", w.Code)
	}
	if w.Message != "something happened" {
		t.Errorf("Warning.Message = %q, want 'something happened'", w.Message)
	}
	if w.Source != src {
		t.Errorf("Warning.Source mismatch")
	}
	if w.TypeName != "MyType" {
		t.Errorf("Warning.TypeName = %q, want MyType", w.TypeName)
	}
}
