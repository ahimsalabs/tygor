package testdata

// AnonymousStructField demonstrates a basic anonymous struct field.
type AnonymousStructField struct {
	Inner struct {
		X int    `json:"x"`
		Y string `json:"y"`
	} `json:"inner"`
	Outer string `json:"outer"`
}

// NestedAnonymousStructs demonstrates nested anonymous structs (2+ levels deep).
type NestedAnonymousStructs struct {
	Level1 struct {
		Level2 struct {
			Level3 struct {
				DeepField string `json:"deep_field"`
			} `json:"level3"`
			Mid string `json:"mid"`
		} `json:"level2"`
		Top string `json:"top"`
	} `json:"level1"`
	Root string `json:"root"`
}

// MultipleAnonymousFields demonstrates multiple anonymous struct fields.
type MultipleAnonymousFields struct {
	First struct {
		A int `json:"a"`
	} `json:"first"`
	Second struct {
		B string `json:"b"`
	} `json:"second"`
}

// AnonymousWithPointers demonstrates anonymous struct with pointer fields.
type AnonymousWithPointers struct {
	Config struct {
		Value  *string `json:"value,omitempty"`
		Count  int     `json:"count"`
		Nested *struct {
			Inner string `json:"inner"`
		} `json:"nested,omitempty"`
	} `json:"config"`
}

// AnonymousWithSliceAndMap demonstrates anonymous struct with complex field types.
type AnonymousWithSliceAndMap struct {
	Data struct {
		Items []string          `json:"items"`
		Props map[string]string `json:"props"`
	} `json:"data"`
}

// AnonymousWithEmbedding demonstrates anonymous struct with embedded field.
type AnonymousWithEmbedding struct {
	Config struct {
		BaseType        // Embedded without tag
		Value    string `json:"value"`
	} `json:"config"`
}

// AnonymousWithNamedEmbedding demonstrates anonymous struct with named embedded field.
type AnonymousWithNamedEmbedding struct {
	Settings struct {
		Base BaseType `json:"base"` // Embedded with tag
		Name string   `json:"name"`
	} `json:"settings"`
}

// CollisionTest_Inner is a named type that would collide with the synthetic name
// generated for CollisionTest.Inner. This should trigger an error.
type CollisionTest_Inner struct {
	Collision string `json:"collision"`
}

// CollisionTest demonstrates name collision detection.
// When extracting this type, the anonymous Inner field would generate
// synthetic name "CollisionTest_Inner", which collides with the existing
// CollisionTest_Inner type above.
type CollisionTest struct {
	Inner struct {
		Z int `json:"z"`
	} `json:"inner"`
}
