package ir

// ServiceDescriptor represents a group of related endpoints.
type ServiceDescriptor struct {
	// Name is the service identifier (e.g., "Users", "Posts").
	Name string

	// Endpoints contains all endpoints in this service.
	Endpoints []EndpointDescriptor

	// Documentation for this service.
	Documentation Documentation
}

// EndpointDescriptor represents a single API endpoint.
type EndpointDescriptor struct {
	// Name is the endpoint identifier within the service (e.g., "Create", "List").
	Name string

	// FullName is the qualified name: "ServiceName.EndpointName" (e.g., "Users.Create").
	FullName string

	// Primitive is the tygor communication primitive: "query", "exec", "stream", or "livevalue".
	//   - "query": cacheable read (HTTP GET)
	//   - "exec": mutation (HTTP POST)
	//   - "stream": server-sent events (HTTP POST + SSE response)
	//   - "livevalue": synchronized state (HTTP POST + SSE response, latest-wins)
	Primitive string

	// Path is the URL path: "/{ServiceName}/{EndpointName}".
	// Example: "/Users/Create", "/News/List"
	Path string

	// Request describes the request payload type.
	// Typically a ReferenceDescriptor pointing to a type in Schema.Types.
	// For query endpoints, fields become query parameters.
	// For exec/stream endpoints, this is the JSON request body.
	// May be nil for endpoints with no request parameters.
	Request TypeDescriptor

	// Response describes the response payload type.
	// For stream endpoints, this is the type of each streamed event.
	// May be a ReferenceDescriptor, ArrayDescriptor, MapDescriptor, etc.
	Response TypeDescriptor

	// JSON is the endpoint's resolved encoding/json/v2 contract after app,
	// service, and endpoint options have been composed. Request semantics apply
	// to JSON-body primitives; query inputs continue to use Gorilla Schema.
	JSON JSONContract

	// Documentation for this endpoint.
	Documentation Documentation
}

// JSONContract is the serializable, directional view of an endpoint's
// resolved native encoding/json/v2 options. Formatting and parser-policy
// fields are retained even when they do not alter generated TypeScript types.
type JSONContract struct {
	Encode JSONEncodeContract `json:"encode"`
	Decode JSONDecodeContract `json:"decode"`
}

type JSONEncodeContract struct {
	StringifyNumbers          bool   `json:"stringifyNumbers"`
	FormatNilSliceAsNull      bool   `json:"formatNilSliceAsNull"`
	FormatNilMapAsNull        bool   `json:"formatNilMapAsNull"`
	OmitZeroStructFields      bool   `json:"omitZeroStructFields"`
	MatchCaseInsensitiveNames bool   `json:"matchCaseInsensitiveNames"`
	AllowDuplicateNames       bool   `json:"allowDuplicateNames"`
	AllowInvalidUTF8          bool   `json:"allowInvalidUTF8"`
	EscapeForHTML             bool   `json:"escapeForHTML"`
	EscapeForJS               bool   `json:"escapeForJS"`
	PreserveRawStrings        bool   `json:"preserveRawStrings"`
	CanonicalizeRawInts       bool   `json:"canonicalizeRawInts"`
	CanonicalizeRawFloats     bool   `json:"canonicalizeRawFloats"`
	ReorderRawObjects         bool   `json:"reorderRawObjects"`
	Deterministic             bool   `json:"deterministic"`
	SpaceAfterColon           bool   `json:"spaceAfterColon"`
	SpaceAfterComma           bool   `json:"spaceAfterComma"`
	Multiline                 bool   `json:"multiline"`
	Indent                    string `json:"indent"`
	IndentPrefix              string `json:"indentPrefix"`
}

type JSONDecodeContract struct {
	StringifyNumbers          bool `json:"stringifyNumbers"`
	MatchCaseInsensitiveNames bool `json:"matchCaseInsensitiveNames"`
	RejectUnknownMembers      bool `json:"rejectUnknownMembers"`
	AllowDuplicateNames       bool `json:"allowDuplicateNames"`
	AllowInvalidUTF8          bool `json:"allowInvalidUTF8"`
}
