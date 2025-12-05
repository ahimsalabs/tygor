// Package testdata contains test types for the source provider.
package testdata

import "time"

// User represents a user in the system.
// This is the full documentation body.
type User struct {
	// ID is the unique identifier
	ID string `json:"id"`

	// Name is the user's display name
	Name string `json:"name"`

	// Email is optional
	Email string `json:"email,omitempty"`

	// Age may be nil
	Age *int `json:"age"`

	// CreatedAt is when the user was created
	CreatedAt time.Time `json:"created_at"`

	// Metadata can contain any JSON
	Metadata map[string]any `json:"metadata,omitempty"`

	// Tags is a list of strings
	Tags []string `json:"tags"`
}

// Status represents user status.
type Status string

const (
	// StatusActive means the user is active
	StatusActive Status = "active"
	// StatusInactive means the user is inactive
	StatusInactive Status = "inactive"
	// StatusPending means awaiting approval
	StatusPending Status = "pending"
	// statusInternal is unexported and should NOT appear in generated enums
	statusInternal Status = "internal"
)

// Priority is an integer enum.
type Priority int

const (
	PriorityLow Priority = iota
	PriorityMedium
	PriorityHigh
)

// SimpleStruct has basic fields.
type SimpleStruct struct {
	StringField string  `json:"string_field"`
	IntField    int     `json:"int_field"`
	BoolField   bool    `json:"bool_field"`
	FloatField  float64 `json:"float_field"`
}

// NestedStruct contains other structs.
type NestedStruct struct {
	User   User         `json:"user"`
	Simple SimpleStruct `json:"simple"`
}

// SliceAndArrayTypes demonstrates arrays and slices.
type SliceAndArrayTypes struct {
	Slice      []string   `json:"slice"`
	Array      [3]int     `json:"array"`
	ByteSlice  []byte     `json:"byte_slice"`
	ByteArray  [16]byte   `json:"byte_array"`
	IntSlice   []int      `json:"int_slice,omitempty"`
	NestedList [][]string `json:"nested_list"`
}

// MapTypes demonstrates map usage.
type MapTypes struct {
	StringMap map[string]int    `json:"string_map"`
	IntMap    map[int]string    `json:"int_map"`
	NestedMap map[string][]User `json:"nested_map,omitempty"`
}

// PointerTypes demonstrates pointer fields.
type PointerTypes struct {
	RequiredPtr *string          `json:"required_ptr"`
	OptionalPtr *string          `json:"optional_ptr,omitempty"`
	StructPtr   *User            `json:"struct_ptr"`
	SlicePtr    *[]string        `json:"slice_ptr"`
	MapPtr      *map[string]int  `json:"map_ptr,omitempty"`
	PtrSlice    []*User          `json:"ptr_slice"`
	PtrMap      map[string]*User `json:"ptr_map"`
	DoublePtr   **string         `json:"double_ptr"`
}

// EmbeddedTypes demonstrates embedding.
type BaseType struct {
	BaseField string `json:"base_field"`
}

type EmbeddedTypes struct {
	BaseType        // Embedded without tag (inheritance)
	OwnField string `json:"own_field"`
}

type NamedEmbedding struct {
	Base BaseType `json:"base"` // Embedded with tag (nested)
	Own  string   `json:"own"`
}

// TaggedFields demonstrates various struct tags.
type TaggedFields struct {
	Skipped       string `json:"-"`
	StringEncoded int    `json:"string_encoded,string"`
	Validated     string `json:"validated" validate:"required,email"`
	OmitZero      []int  `json:"omit_zero,omitzero"`
	CustomTags    string `json:"custom" db:"custom_db" xml:"CustomXML"`
}

// Deprecated: OldStruct is deprecated, use User instead.
type OldStruct struct {
	Field string `json:"field"`
}

// EmptyStruct has no fields.
type EmptyStruct struct{}

// InterfaceField contains an interface.
type InterfaceField struct {
	Any interface{} `json:"any"`
}
