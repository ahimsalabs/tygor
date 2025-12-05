package ir

import "testing"

func TestServiceDescriptor(t *testing.T) {
	svc := ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/Users/Create",
				Request:   Ref("CreateUserRequest", "api"),
				Response:  Ref("User", "api"),
			},
			{
				Name:      "Get",
				FullName:  "Users.Get",
				Primitive: "query",
				Path:      "/Users/Get",
				Request:   Ref("GetUserRequest", "api"),
				Response:  Ref("User", "api"),
			},
		},
		Documentation: Documentation{Summary: "User management service"},
	}

	if svc.Name != "Users" {
		t.Errorf("ServiceDescriptor.Name = %q, want Users", svc.Name)
	}
	if len(svc.Endpoints) != 2 {
		t.Errorf("ServiceDescriptor.Endpoints length = %d, want 2", len(svc.Endpoints))
	}
	if svc.Documentation.Summary != "User management service" {
		t.Errorf("ServiceDescriptor.Documentation.Summary = %q", svc.Documentation.Summary)
	}
}

func TestEndpointDescriptor_POST(t *testing.T) {
	ep := EndpointDescriptor{
		Name:      "Create",
		FullName:  "Users.Create",
		Primitive: "exec",
		Path:      "/Users/Create",
		Request:   Ref("CreateUserRequest", "api"),
		Response:  Ref("User", "api"),
		Documentation: Documentation{
			Summary: "Create a new user",
			Body:    "Create a new user with the given details.",
		},
	}

	if ep.Name != "Create" {
		t.Errorf("EndpointDescriptor.Name = %q, want Create", ep.Name)
	}
	if ep.FullName != "Users.Create" {
		t.Errorf("EndpointDescriptor.FullName = %q, want Users.Create", ep.FullName)
	}
	if ep.Primitive != "exec" {
		t.Errorf("EndpointDescriptor.Primitive = %q, want exec", ep.Primitive)
	}
	if ep.Path != "/Users/Create" {
		t.Errorf("EndpointDescriptor.Path = %q, want /Users/Create", ep.Path)
	}
	if ep.Request.Kind() != KindReference {
		t.Errorf("EndpointDescriptor.Request.Kind() = %v, want KindReference", ep.Request.Kind())
	}
	if ep.Response.Kind() != KindReference {
		t.Errorf("EndpointDescriptor.Response.Kind() = %v, want KindReference", ep.Response.Kind())
	}
}

func TestEndpointDescriptor_GET(t *testing.T) {
	ep := EndpointDescriptor{
		Name:      "List",
		FullName:  "Posts.List",
		Primitive: "query",
		Path:      "/Posts/List",
		Request:   Ref("ListPostsParams", "api"),
		Response:  Slice(Ref("Post", "api")),
	}

	if ep.Primitive != "query" {
		t.Errorf("EndpointDescriptor.Primitive = %q, want query", ep.Primitive)
	}
	if ep.Response.Kind() != KindArray {
		t.Errorf("EndpointDescriptor.Response.Kind() = %v, want KindArray", ep.Response.Kind())
	}
}

func TestEndpointDescriptor_NilRequest(t *testing.T) {
	// Endpoints with no request parameters have nil Request
	ep := EndpointDescriptor{
		Name:      "Health",
		FullName:  "System.Health",
		Primitive: "query",
		Path:      "/System/Health",
		Request:   nil,
		Response:  Ref("HealthResponse", "api"),
	}

	if ep.Request != nil {
		t.Error("EndpointDescriptor.Request should be nil for no-param endpoints")
	}
}

func TestEndpointDescriptor_VoidResponse(t *testing.T) {
	// Endpoints returning void use *struct{}
	ep := EndpointDescriptor{
		Name:      "Delete",
		FullName:  "Users.Delete",
		Primitive: "exec",
		Path:      "/Users/Delete",
		Request:   Ref("DeleteUserRequest", "api"),
		Response:  Ptr(Empty()), // *struct{} -> null on wire
	}

	if ep.Response.Kind() != KindPtr {
		t.Errorf("EndpointDescriptor.Response.Kind() = %v, want KindPtr", ep.Response.Kind())
	}
	ptr := ep.Response.(*PtrDescriptor)
	if ptr.Element.Kind() != KindPrimitive {
		t.Errorf("inner kind = %v, want KindPrimitive", ptr.Element.Kind())
	}
	prim := ptr.Element.(*PrimitiveDescriptor)
	if prim.PrimitiveKind != PrimitiveEmpty {
		t.Errorf("primitive kind = %v, want PrimitiveEmpty", prim.PrimitiveKind)
	}
}
