package ir

import "encoding/json"

// JSON serialization support for IR types.
// All types include a "kind" field for type discrimination.

// MarshalJSON implements json.Marshaler for StructDescriptor.
func (d *StructDescriptor) MarshalJSON() ([]byte, error) {
	type Alias StructDescriptor
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "struct",
		Alias: (*Alias)(d),
	})
}

// MarshalJSON implements json.Marshaler for AliasDescriptor.
func (d *AliasDescriptor) MarshalJSON() ([]byte, error) {
	type Alias AliasDescriptor
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "alias",
		Alias: (*Alias)(d),
	})
}

// MarshalJSON implements json.Marshaler for EnumDescriptor.
func (d *EnumDescriptor) MarshalJSON() ([]byte, error) {
	type Alias EnumDescriptor
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "enum",
		Alias: (*Alias)(d),
	})
}

// MarshalJSON implements json.Marshaler for PrimitiveDescriptor.
func (d *PrimitiveDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind          string `json:"kind"`
		PrimitiveKind string `json:"primitiveKind"`
		BitSize       int    `json:"bitSize,omitempty"`
	}{
		Kind:          "primitive",
		PrimitiveKind: d.PrimitiveKind.String(),
		BitSize:       d.BitSize,
	})
}

// MarshalJSON implements json.Marshaler for ArrayDescriptor.
func (d *ArrayDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind    string         `json:"kind"`
		Element TypeDescriptor `json:"element"`
		Length  int            `json:"length"`
	}{
		Kind:    "array",
		Element: d.Element,
		Length:  d.Length,
	})
}

// MarshalJSON implements json.Marshaler for MapDescriptor.
func (d *MapDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind  string         `json:"kind"`
		Key   TypeDescriptor `json:"key"`
		Value TypeDescriptor `json:"value"`
	}{
		Kind:  "map",
		Key:   d.Key,
		Value: d.Value,
	})
}

// MarshalJSON implements json.Marshaler for ReferenceDescriptor.
func (d *ReferenceDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		Pkg  string `json:"package,omitempty"`
	}{
		Kind: "reference",
		Name: d.Target.Name,
		Pkg:  d.Target.Package,
	})
}

// MarshalJSON implements json.Marshaler for PtrDescriptor.
func (d *PtrDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind    string         `json:"kind"`
		Element TypeDescriptor `json:"element"`
	}{
		Kind:    "ptr",
		Element: d.Element,
	})
}

// MarshalJSON implements json.Marshaler for UnionDescriptor.
func (d *UnionDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind  string           `json:"kind"`
		Types []TypeDescriptor `json:"types"`
	}{
		Kind:  "union",
		Types: d.Types,
	})
}

// MarshalJSON implements json.Marshaler for TypeParameterDescriptor.
func (d *TypeParameterDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind       string         `json:"kind"`
		ParamName  string         `json:"paramName"`
		Constraint TypeDescriptor `json:"constraint,omitempty"`
	}{
		Kind:       "typeParameter",
		ParamName:  d.ParamName,
		Constraint: d.Constraint,
	})
}

// MarshalJSON implements json.Marshaler for GoIdentifier.
func (id GoIdentifier) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name    string `json:"name"`
		Package string `json:"package,omitempty"`
	}{
		Name:    id.Name,
		Package: id.Package,
	})
}

// MarshalJSON implements json.Marshaler for FieldDescriptor.
func (f FieldDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name          string         `json:"name"`
		Type          TypeDescriptor `json:"type"`
		JSONName      string         `json:"jsonName"`
		Optional      bool           `json:"optional,omitempty"`
		StringEncoded bool           `json:"stringEncoded,omitempty"`
		Skip          bool           `json:"skip,omitempty"`
		ValidateTag   string         `json:"validateTag,omitempty"`
		Doc           string         `json:"doc,omitempty"`
	}{
		Name:          f.Name,
		Type:          f.Type,
		JSONName:      f.JSONName,
		Optional:      f.Optional,
		StringEncoded: f.StringEncoded,
		Skip:          f.Skip,
		ValidateTag:   f.ValidateTag,
		Doc:           f.Documentation.Summary,
	})
}

// MarshalJSON implements json.Marshaler for EndpointDescriptor.
func (e EndpointDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name      string         `json:"name"`
		FullName  string         `json:"fullName"`
		Primitive string         `json:"primitive"`
		Path      string         `json:"path"`
		Request   TypeDescriptor `json:"request,omitempty"`
		Response  TypeDescriptor `json:"response,omitempty"`
		Doc       string         `json:"doc,omitempty"`
	}{
		Name:      e.Name,
		FullName:  e.FullName,
		Primitive: e.Primitive,
		Path:      e.Path,
		Request:   e.Request,
		Response:  e.Response,
		Doc:       e.Documentation.Summary,
	})
}

// MarshalJSON implements json.Marshaler for ServiceDescriptor.
func (s ServiceDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name      string               `json:"name"`
		Endpoints []EndpointDescriptor `json:"endpoints"`
		Doc       string               `json:"doc,omitempty"`
	}{
		Name:      s.Name,
		Endpoints: s.Endpoints,
		Doc:       s.Documentation.Summary,
	})
}

// MarshalJSON implements json.Marshaler for EnumMember.
func (m EnumMember) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
		Doc   string `json:"doc,omitempty"`
	}{
		Name:  m.Name,
		Value: m.Value,
		Doc:   m.Documentation.Summary,
	})
}
