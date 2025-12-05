<!-- snippet-lint-disable -->

# Tygor Protocol Specification

**Version:** 1.1
**Status:** Draft

## 1. Introduction

This document specifies the wire protocol for **tygor**, a type-safe API framework. Any implementation (server or client, in any language) that conforms to this specification is interoperable with other conforming implementations.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

---

## 2. Core Concepts

### 2.1 Operations

An **Operation** is uniquely identified by a **Service Name** and **Method Name** pair.

- **Service Name**: A string identifying a logical grouping of related operations (e.g., `"News"`, `"Users"`).
- **Method Name**: A string identifying a specific operation within a service (e.g., `"List"`, `"Get"`, `"Create"`).
- **Operation ID**: The canonical identifier format is `"{ServiceName}.{MethodName}"` (e.g., `"News.List"`).

### 2.2 Request and Response Types

Each operation has:
- A **Request Type**: A structured data type containing input parameters
- A **Response Type**: A structured data type containing the result

---

## 3. HTTP Transport

### 3.1 URL Path Construction

The HTTP endpoint for an operation MUST be constructed as:

```
/{ServiceName}/{MethodName}
```

**Examples:**
- `GET /News/List`
- `POST /Users/Create`

Path segments are case-sensitive and MUST match the service and method names exactly.

### 3.2 HTTP Methods

Operations MUST use one of two HTTP methods based on their semantics:

#### 3.2.1 GET Requests

**Usage:** MUST be used for read-only operations (queries that do not modify state).

**Characteristics:**
- Cacheable via standard HTTP cache headers
- Idempotent and safe
- Parameters visible in URLs (suitable for non-sensitive data)

**Request Encoding:**
- Request parameters MUST be encoded in the URL query string
- Field names MUST match the request type's field names
- Arrays MUST be serialized using the "repeat" convention: `?ids=1&ids=2&ids=3`
- Nested objects MAY be serialized using bracket notation: `?user[name]=alice&user[age]=30`

**Example:**
```
GET /News/List?limit=10&offset=0&tags=tech&tags=go
```

#### 3.2.2 POST Requests

**Usage:** MUST be used for all state-changing operations (create, update, delete, mutations).

**Rationale:** In an RPC system, operation semantics are conveyed by the service and method name (e.g., `News.Create`, `News.Update`, `News.Delete`), not by HTTP verbs. Using POST for all mutations simplifies the protocol while maintaining clear intent through naming.

**Characteristics:**
- Not cacheable
- May modify server state
- Parameters in request body (suitable for sensitive data)

**Request Encoding:**
- Request parameters MUST be encoded as JSON in the request body
- Content-Type header MUST be `application/json`
- An empty request body MUST be treated as equivalent to `{}` (empty object)

**Example:**
```
POST /News/Create
Content-Type: application/json

{
  "title": "Hello World",
  "body": "This is a post",
  "tags": ["tech", "go"]
}
```

**Note:** While implementations MAY support other HTTP methods (PUT, PATCH, DELETE) for REST-style conventions, conforming implementations need only support GET and POST.

### 3.3 Response Format

All responses MUST use a discriminated envelope structure with either a `result` field (success) or an `error` field (failure), but never both. This enables clients to handle responses without relying on HTTP status codes for control flow, supporting "never throw" API patterns.

**Rationale:** While HTTP status codes remain authoritative for transport-level semantics (load balancers, proxies, observability), the envelope provides a uniform structure for application-level response handling. Clients can always parse the same top-level shape and discriminate via `result`/`error` presence.

#### 3.3.1 Success Responses

**HTTP Status:** `200 OK`

**Headers:**
- `Content-Type: application/json`

**Body:** JSON object with a `result` field containing the response data.

```typescript
{
  "result": T  // Response type, or null for void operations
}
```

**Example (data response):**
```json
{
  "result": {
    "id": 123,
    "title": "Hello World",
    "createdAt": "2024-01-15T10:30:00Z"
  }
}
```

**Example (void response):**
```json
{
  "result": null
}
```

**Example (array response):**
```json
{
  "result": [
    {"id": 1, "title": "First"},
    {"id": 2, "title": "Second"}
  ]
}
```

#### 3.3.2 Error Responses

**HTTP Status:** Determined by error code (see Section 4.2). Servers MUST set the appropriate HTTP status code for observability and infrastructure compatibility.

**Headers:**
- `Content-Type: application/json`

**Body:** JSON object with an `error` field containing the error envelope (see Section 4.1).

---

## 4. Error Handling

### 4.1 Error Envelope Structure

All errors MUST be returned wrapped in an `error` field, containing a JSON object with the following structure:

```typescript
{
  "error": {
    "code": string,      // Error code (see 4.2)
    "message": string,   // Human-readable error message
    "details"?: object   // Optional additional error context
  }
}
```

**Field Descriptions:**
- **`code`** (REQUIRED): A machine-readable error code from the standardized set (Section 4.2)
- **`message`** (REQUIRED): A human-readable description of the error
- **`details`** (OPTIONAL): A map of additional context, structure is error-specific

**Example:**
```json
{
  "error": {
    "code": "invalid_argument",
    "message": "title field is required",
    "details": {
      "field": "title",
      "reason": "missing_required_field"
    }
  }
}
```

**TypeScript Response Type:**

The discriminated union enables type-safe response handling:

```typescript
type RPCResponse<T> =
  | { result: T; error?: never }
  | { result?: never; error: RPCError };

interface RPCError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}
```

### 4.2 Standard Error Codes

The following error codes MUST be supported. Implementations MAY define additional codes, but SHOULD prefer using these standard codes with appropriate `details` for specificity.

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `invalid_argument` | 400 Bad Request | Request parameters are invalid or malformed |
| `unauthenticated` | 401 Unauthorized | Request lacks valid authentication credentials |
| `permission_denied` | 403 Forbidden | Authenticated user lacks permission for this operation |
| `not_found` | 404 Not Found | Requested resource does not exist |
| `method_not_allowed` | 405 Method Not Allowed | HTTP method not allowed for this operation |
| `conflict` | 409 Conflict | Operation conflicts with current resource state |
| `already_exists` | 409 Conflict | Resource already exists (alias for conflict) |
| `gone` | 410 Gone | Resource permanently deleted |
| `resource_exhausted` | 429 Too Many Requests | Rate limit exceeded or quota exhausted |
| `canceled` | 499 Client Closed Request | Request canceled by client |
| `internal` | 500 Internal Server Error | Unspecified server error |
| `not_implemented` | 501 Not Implemented | Operation not implemented |
| `unavailable` | 503 Service Unavailable | Service temporarily unavailable |
| `deadline_exceeded` | 504 Gateway Timeout | Request timeout exceeded |

### 4.3 Error Code Extension

Implementations MAY define custom error codes for domain-specific errors. Custom codes:
- SHOULD follow `snake_case` naming convention
- SHOULD map to an appropriate HTTP status code
- SHOULD be documented in the service's API documentation

---

## 5. Type System

### 5.1 Primitive Types

The protocol supports the following primitive types in request/response bodies:

| Type | JSON Representation | Notes |
|------|---------------------|-------|
| Boolean | `true` / `false` | |
| Integer | JSON number | No fractional part |
| Float | JSON number | May have fractional part |
| String | JSON string | UTF-8 encoded |
| Bytes | Base64-encoded string | Binary data |
| Timestamp | ISO 8601 string | RFC 3339 format (e.g., `"2024-01-15T10:30:00Z"`) |

### 5.2 Complex Types

- **Object:** JSON object with named fields
- **Array:** JSON array containing zero or more elements
- **Null/Optional:** Fields MAY be omitted or set to `null` to indicate absence

### 5.3 Empty Type

Operations that do not return meaningful data (void operations) MUST use the **Empty** type, which serializes to JSON `null`.

**Go:**
```go
type Empty *struct{}  // nil is the zero value, serializes to JSON null
```

**TypeScript:**
```typescript
type Empty = null;
```

**Wire format:**
```json
{"result": null}
```

**Usage example (Go):**
```go
func DeleteUser(ctx context.Context, req *DeleteUserRequest) (tygor.Empty, error) {
    // ... delete user
    return nil, nil
}
```

### 5.4 Type Schema

While this protocol does not mandate a specific schema definition language, implementations SHOULD provide machine-readable type definitions for all operations (see Section 6).

---

## 6. Metadata and Discovery

### 6.1 Manifest File

Implementations that generate client code SHOULD provide a **manifest file** that describes all available operations and their types.

**Recommended Format (TypeScript):**

```typescript
// manifest.ts
import { ServiceRegistry } from '@tygor/client';
import * as types from './types';

export interface Manifest {
  "ServiceName.MethodName": {
    req: types.RequestType;
    res: types.ResponseType;
  };
  // ... additional operations
}

const metadata = {
  "ServiceName.MethodName": { path: "/ServiceName/MethodName", primitive: "query" },
  "ServiceName.OtherMethod": { path: "/ServiceName/OtherMethod", primitive: "exec" },
  // ... additional operations
} as const;

export const registry: ServiceRegistry<Manifest> = {
  manifest: {} as Manifest,
  metadata,
};
```

**Purpose:**
- Enables type-safe client generation with full type inference
- Provides operation discovery without runtime introspection
- Documents the API contract
- Single source of truth for service definitions

### 6.2 Runtime Discovery (Optional)

Implementations MAY provide a runtime discovery endpoint (e.g., `GET /.well-known/tygor/manifest.json`) that returns operation metadata in JSON format.

---

## 7. Content Negotiation

### 7.1 Request Headers

Clients SHOULD send:
```
Content-Type: application/json  (for POST requests)
Accept: application/json
```

### 7.2 Response Headers

Servers MUST send:
```
Content-Type: application/json
```

### 7.3 Character Encoding

All JSON content MUST use UTF-8 encoding.

---

## 8. Caching

### 8.1 GET Request Caching

Operations using `GET` SHOULD be cacheable. Servers MAY include standard HTTP caching headers as defined in RFC 9111 (HTTP Caching).

**Common Cache-Control Directives:**

```
Cache-Control: public, max-age=300, stale-while-revalidate=60
```

**Supported Directives:**
- `max-age=<seconds>`: Maximum time resource is considered fresh
- `s-maxage=<seconds>`: Like max-age but only for shared caches (CDNs)
- `public`: Response may be cached by any cache
- `private`: Response specific to user, only browser cache
- `stale-while-revalidate=<seconds>`: Serve stale content while revalidating in background
- `stale-if-error=<seconds>`: Serve stale content if origin is down
- `must-revalidate`: Once stale, must revalidate before use
- `immutable`: Resource will never change

**ETag Support:**

Servers MAY implement ETag-based validation:

```
ETag: "abc123"
```

Clients can use `If-None-Match` for conditional requests:

```
If-None-Match: "abc123"
```

Server responds with `304 Not Modified` if content unchanged.

### 8.2 POST Request Caching

Operations using `POST` MUST NOT be cached. Responses to POST requests are not cacheable by default per HTTP semantics.

---

## 9. Versioning

This protocol does not specify a versioning mechanism. Service providers SHOULD:
- Maintain backward compatibility when possible
- Use URL path prefixes for versioned APIs (e.g., `/v2/News/List`)
- Document breaking changes clearly

---

## 10. Security Considerations

### 10.1 Authentication

This protocol does not mandate an authentication mechanism. Implementations SHOULD use standard HTTP authentication methods:
- Bearer tokens (e.g., `Authorization: Bearer <token>`)
- Cookies
- Client certificates

### 10.2 HTTPS

Production deployments MUST use HTTPS to protect data in transit.

### 10.3 Input Validation

Servers MUST validate all inputs and reject malformed requests with `invalid_argument` (400).

---

## 11. Examples

### 11.1 Successful GET Request

**Request:**
```http
GET /News/List?limit=2&category=tech HTTP/1.1
Host: api.example.com
Accept: application/json
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "result": [
    {"id": 1, "title": "Go 1.22 Released", "category": "tech"},
    {"id": 2, "title": "gRPC vs REST", "category": "tech"}
  ]
}
```

### 11.2 Successful POST Request

**Request:**
```http
POST /News/Create HTTP/1.1
Host: api.example.com
Content-Type: application/json

{
  "title": "New Article",
  "body": "Article content here",
  "category": "tech"
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "result": {
    "id": 123,
    "title": "New Article",
    "createdAt": "2024-01-15T10:30:00Z"
  }
}
```

### 11.3 Void Response

**Request:**
```http
POST /News/Delete HTTP/1.1
Host: api.example.com
Content-Type: application/json

{
  "id": 123
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "result": null
}
```

### 11.4 Error Response

**Request:**
```http
POST /News/Create HTTP/1.1
Host: api.example.com
Content-Type: application/json

{
  "body": "Missing title field"
}
```

**Response:**
```http
HTTP/1.1 400 Bad Request
Content-Type: application/json

{
  "error": {
    "code": "invalid_argument",
    "message": "title is required",
    "details": {
      "field": "title",
      "constraint": "required"
    }
  }
}
```

---

## 12. Compliance

An implementation is **protocol-compliant** if it:

1. ✅ Constructs URLs as `/{Service}/{Method}`
2. ✅ Uses GET for read-only operations with query string parameters
3. ✅ Uses POST for state-changing operations with JSON body
4. ✅ Serializes arrays in GET requests using the repeat format (`?id=1&id=2`)
5. ✅ Wraps success responses in `{"result": ...}` envelope
6. ✅ Wraps error responses in `{"error": {...}}` envelope with `code` and `message` fields
7. ✅ Maps error codes to the specified HTTP status codes
8. ✅ Uses `application/json` content type for requests and responses
9. ✅ Encodes timestamps as ISO 8601 strings (RFC 3339 format)
10. ✅ Respects HTTP caching semantics (GET cacheable, POST not cacheable)
11. ✅ Uses `null` for void/empty response results

---

## Appendix A: ABNF Grammar

```abnf
operation-path = "/" service-name "/" method-name
service-name   = 1*ALPHA *(ALPHA / DIGIT / "_")
method-name    = 1*ALPHA *(ALPHA / DIGIT / "_")
```

**Note:** While underscores are permitted by the grammar, PascalCase is RECOMMENDED for service and method names (e.g., `"News"`, `"UserProfile"`, `"ListActive"`) to align with common API conventions and code generation patterns.

---

## Appendix B: Future Work

The following features are under consideration for future protocol versions:

### B.1 Request Batching

Batching multiple RPC calls in a single HTTP request would reduce latency for clients making several related calls. This would require:

- An `id` field in requests and responses for correlation
- Array-based request/response format: `[{id, method, params}, ...]` → `[{id, result/error}, ...]`
- HTTP 207 Multi-Status for mixed success/error responses
- Follows JSON-RPC 2.0 batching conventions

### B.2 Streaming and Bidirectional Communication

WebSocket support for:

- Server-sent events (subscriptions, real-time updates)
- Bidirectional streaming (chat, collaborative editing)
- Long-lived connections with multiplexed RPCs

This would extend the protocol to support `stream` and `bidi` operation types alongside the current `unary` operations.

---

## Appendix C: Changelog

- **v1.1 (2025-01-XX)**: Added response envelope (`result`/`error` discrimination), Empty type for void operations
- **v1.0 (2024-01-15)**: Initial protocol specification
