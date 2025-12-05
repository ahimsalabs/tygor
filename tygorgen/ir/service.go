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

	// Primitive is the tygor communication primitive: "query", "exec", "stream", or "atom".
	//   - "query": cacheable read (HTTP GET)
	//   - "exec": mutation (HTTP POST)
	//   - "stream": server-sent events (HTTP POST + SSE response)
	//   - "atom": synchronized state (HTTP POST + SSE response, latest-wins)
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

	// Documentation for this endpoint.
	Documentation Documentation
}
