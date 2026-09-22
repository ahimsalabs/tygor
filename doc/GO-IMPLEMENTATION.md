<!-- snippet-lint-disable -->

# Tygor Go Implementation Specification

**Version:** 1.0
**Status:** Draft

## 1. Introduction

This document specifies the implementation requirements for the **tygor** Go library (`tygor.dev`). This library provides a code-first RPC framework that implements the [Tygor Protocol](./PROTOCOL.md).

For wire protocol details and interoperability requirements, see **PROTOCOL.md**.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

---

## 2. Data Layer Integration (`sqlc`)

The Go implementation is designed to work seamlessly with `sqlc` for type-safe database interactions.

### 2.1 Recommended `sqlc` Configuration

The following `sqlc.yaml` configuration is RECOMMENDED to ensure optimal integration:

```yaml
version: "2"
sql:
  - schema: "schema.sql"
    queries: "queries.sql"
    engine: "postgresql"
    gen:
      go:
        package: "db"
        out: "internal/db"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_result_struct_pointers: true
        emit_params_struct_pointers: true
        query_parameter_limit: 0  # Force parameter structs for all queries
```

**Rationale:**
- **`query_parameter_limit: 0`**: Forces `sqlc` to generate a dedicated parameters struct for every query, making them suitable for use as RPC request types
- **`emit_result_struct_pointers: true`**: Nullable SQL rows map to Go pointers, compatible with tygor's optional field handling
- **`emit_params_struct_pointers: true`**: Input parameters use pointers for optional fields
- **`emit_json_tags: true`**: Generated structs include JSON tags for automatic serialization

### 2.2 Query Naming Convention

Query names SHOULD follow `PascalCase` to align with Go naming conventions:

```sql
-- name: GetUser :one
-- name: ListUsers :many
-- name: CreateUser :one
```

These generated structs can then be used directly as RPC request/response types.

---

## 3. Handler Definition

### 3.1 Handler Function Signature

All RPC handler functions MUST conform to the following signature:

```go
func(ctx context.Context, req Req) (Res, error)
```

Where:
- `ctx` is the request context
- `Req` is the request type (typically a `sqlc`-generated struct or custom type)
- `Res` is the response type
- `error` is returned for any failure

**Example:**
```go
func ListNews(ctx context.Context, req *db.ListNewsParams) ([]*db.News, error) {
    queries := db.New(pool) // assumes pgx pool available
    return queries.ListNews(ctx, req)
}
```

### 3.2 Typed Registration and Options

Handlers are registered through generic `Service` methods. Each method accepts
only its compatible sealed option family:

```go
func (s *Service) Exec[Req, Res any](name string, fn func(context.Context, Req) (Res, error), options ...ExecOption)
func (s *Service) Query[Req, Res any](name string, fn func(context.Context, Req) (Res, error), options ...QueryOption)
func (s *Service) Stream[Req, Res any](name string, fn func(context.Context, Req, StreamWriter[Res]) error, options ...StreamOption)
func (s *Service) LiveValue[T any](name string, value *LiveValue[T], options ...LiveValueOption)
```

Options are applied left to right before the endpoint is published. Shared
options implement only the families where they are meaningful; for example,
`WithCacheControl` is a `QueryOption`, while `WithStreamHeartbeat` applies to
stream and live-value endpoints as well as app and service defaults.

**Example Usage:**
```go
// POST handler (default for mutations)
news.Exec("Create", CreateNews)

// GET handler with caching
news.Query("List", ListNews, tygor.WithCacheControl(tygor.CacheConfig{
    MaxAge: 5 * time.Minute,
    Public: true,
}))
```

**Defaults:**
- `Service.Exec()`: POST method, no caching, validation enabled
- `Service.Query()`: GET method, no caching, validation enabled, lenient query params

---

## 4. App and Service Registration

### 4.1 App

The library MUST provide an `App` type that manages services and routes.

```go
type App struct {
    // internal fields
}

func NewApp(options ...AppOption) *App
```

### 4.2 Service Namespacing

The `App.Service(name string, options ...ServiceOption)` method MUST return a
`Service` instance that provides a namespace for related operations.

```go
type Service struct {
    // internal fields
}

func (r *App) Service(name string, options ...ServiceOption) *Service
```

### 4.3 Typed Handler Registration

`Service` MUST expose typed methods that apply options and register complete,
immutable handlers before publishing them.

```go
func (s *Service) Exec[Req, Res any](method string, fn func(context.Context, Req) (Res, error), options ...ExecOption)
func (s *Service) Query[Req, Res any](method string, fn func(context.Context, Req) (Res, error), options ...QueryOption)
func (s *Service) Stream[Req, Res any](method string, fn func(context.Context, Req, StreamWriter[Res]) error, options ...StreamOption)
func (s *Service) LiveValue[T any](method string, value *LiveValue[T], options ...LiveValueOption)
```

**Example:**
```go
app := tygor.NewApp()
news := app.Service("News")
news.Query("List", ListNews)
news.Exec("Create", CreateNews)
```

This registers operations:
- `GET /News/List`
- `POST /News/Create`

### 4.4 HTTP Integration

The `App` MUST provide a `Handler()` method that returns an `http.Handler` for use with the standard library.

```go
func (r *App) Handler() http.Handler
```

**Example:**
```go
http.ListenAndServe(":8080", app.Handler())
```

---

## 5. Configuration API

### 5.1 App Configuration

The `App` MUST accept sealed options during construction:

```go
func WithErrorTransformer(fn ErrorTransformer) AppOption
func WithMaskInternalErrors() AppOption
func WithUnaryInterceptors(interceptors ...UnaryInterceptor) UnaryInterceptorOption
func WithHTTPMiddleware(middlewares ...func(http.Handler) http.Handler) AppOption
func WithLogger(logger *slog.Logger) AppOption
func WithMaxRequestBodySize(size uint64) RequestBodySizeOption
func WithStreamWriteTimeout(timeout time.Duration) StreamTimingOption
func WithStreamHeartbeat(interval time.Duration) StreamTimingOption
```

**Example:**
```go
app := tygor.NewApp(
    tygor.WithErrorTransformer(customErrorHandler),
    tygor.WithLogger(slog.Default()),
    tygor.WithMaskInternalErrors(),
)
```

### 5.2 Service Configuration

The `Service` type SHOULD accept scoped defaults at creation:

```go
service := app.Service("News",
    tygor.WithUnaryInterceptors(authInterceptor),
    tygor.WithMaxRequestBodySize(1<<20),
    tygor.WithStreamInterceptors(metricsInterceptor),
)
```

---

## 6. Error Handling

### 6.1 Error Structure

The library MUST provide an `Error` type conforming to the protocol:

```go
type Error struct {
    Code    ErrorCode      `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string
```

### 6.2 Error Codes

The library MUST define an `ErrorCode` type with constants for all standard protocol error codes:

```go
type ErrorCode string

const (
    CodeInvalidArgument   ErrorCode = "invalid_argument"
    CodeUnauthenticated   ErrorCode = "unauthenticated"
    CodePermissionDenied  ErrorCode = "permission_denied"
    CodeNotFound          ErrorCode = "not_found"
    CodeMethodNotAllowed  ErrorCode = "method_not_allowed"
    CodeConflict          ErrorCode = "conflict"
    CodeAlreadyExists     ErrorCode = "already_exists" // Alias for conflict
    CodeGone              ErrorCode = "gone"
    CodeResourceExhausted ErrorCode = "resource_exhausted"
    CodeCanceled          ErrorCode = "canceled"
    CodeInternal          ErrorCode = "internal"
    CodeNotImplemented    ErrorCode = "not_implemented"
    CodeUnavailable       ErrorCode = "unavailable"
    CodeDeadlineExceeded  ErrorCode = "deadline_exceeded"
)
```

### 6.3 Error Constructor Functions

The library provides constructor functions and chainable methods for adding details:

```go
func NewError(code ErrorCode, message string) *Error
func Errorf(code ErrorCode, format string, args ...any) *Error

func (e *Error) WithDetail(key string, value any) *Error
func (e *Error) WithDetails(details map[string]any) *Error
```

**Example:**
```go
return tygor.NewError(tygor.CodeNotFound, "user not found").
    WithDetail("user_id", id)
```

### 6.4 Error Transformer

The library MUST support custom error transformation via an `ErrorTransformer` function:

```go
type ErrorTransformer func(error) *Error
```

**Default Transformation Behavior:**
The default transformer MUST implement the following mappings:

```go
func DefaultErrorTransformer(err error) *Error {
    if err == nil {
        return nil
    }

    // Already a tygor.Error
    var rpcErr *Error
    if errors.As(err, &rpcErr) {
        return rpcErr
    }

    // Context errors
    if errors.Is(err, context.DeadlineExceeded) {
        return NewError(CodeDeadlineExceeded, "request timeout")
    }
    if errors.Is(err, context.Canceled) {
        return NewError(CodeCanceled, "context canceled")
    }

    // Validation errors (go-playground/validator)
    var valErrs validator.ValidationErrors
    if errors.As(err, &valErrs) {
        details := make(map[string]any)
        for _, ve := range valErrs {
            details[ve.Field()] = ve.Tag()
        }
        return &Error{
            Code:    CodeInvalidArgument,
            Message: "validation failed",
            Details: details,
        }
    }

    // Default: internal error (preserves original message)
    return NewError(CodeInternal, err.Error())
}
```

**Configuration:**
```go
app := tygor.NewApp().
    WithErrorTransformer(func(err error) *tygor.Error {
        // Custom transformation
        if errors.Is(err, sql.ErrNoRows) {
            return tygor.NewError(tygor.CodeNotFound, "resource not found")
        }
        return nil // Fall back to default transformer
    })
```

---

## 7. Request Decoding

### 7.1 Query String Decoding (GET)

For GET requests, the library MUST decode query parameters using a decoder compatible with `gorilla/schema`, configured to use `json` tags via `SetAliasTag("json")`.

**Important:** GET requests use `json` struct tags, just like POST request bodies. This ensures TypeScript clients can use the same property names for both request types. The query parameter decoder is configured with `SetAliasTag("json")` to read from json tags instead of the default schema tags.

**Case-Insensitive Matching:**
Query parameter names are matched case-insensitively. Without a `json` tag, the field name is used (e.g., field `Limit` matches query param `limit`, `Limit`, or `LIMIT`).

**Array Handling:**
Arrays MUST be decoded from the "repeat" format: `?ids=1&ids=2&ids=3`

**Example:**
```go
type ListParams struct {
    Limit  int      `json:"limit"`
    Offset int      `json:"offset"`
    Tags   []string `json:"tags"`
}

// GET /News/List?limit=10&offset=0&tags=go&tags=tech
// Decodes to: ListParams{Limit: 10, Offset: 0, Tags: []string{"go", "tech"}}
```

### 7.2 JSON Body Decoding (POST)

For POST requests, the library MUST decode JSON bodies using `encoding/json`.

**Empty Body Handling:**
An empty request body (EOF) MUST leave the request at its Go zero value. Normal
request validation still applies. In particular, an empty body leaves an
ordinary pointer request nil, while `{}` allocates the pointed-to value. This
allows clients to call endpoints using `tygor.Empty` without explicitly sending
`{}`:

```bash
# Both are valid for endpoints with empty request types:
curl -X POST http://localhost:8080/Tasks/Kill
curl -X POST http://localhost:8080/Tasks/Kill -d '{}'
```

**Single Value and Null Handling:**
A non-empty body MUST contain exactly one JSON value. After decoding that value,
the decoder MUST require EOF; JSON whitespace before EOF is allowed. A trailing
value or trailing non-whitespace data is an invalid argument.

JSON `null` uses the normal `encoding/json` behavior. It leaves a pointer
request nil, which request validation rejects as an invalid argument unless
validation is disabled. `tygor.Empty` is the intentional exception and accepts
empty, `null`, and `{}` bodies.

**Pointer Field Handling:**
Pointer fields SHOULD support `omitempty` to distinguish between "not provided" and "explicitly null":

```go
type UpdateUserParams struct {
    Name  *string `json:"name,omitempty"`
    Email *string `json:"email,omitempty"`
}
```

---

## 8. Validation

### 8.1 Automatic Validation

If `github.com/go-playground/validator/v10` is available, the library SHOULD automatically validate request structs before calling the handler.

**Validation Tags:**
```go
type CreateUserParams struct {
    Email    string `json:"email" validate:"required,email"`
    Username string `json:"username" validate:"required,min=3,max=20"`
    Age      int    `json:"age" validate:"gte=0,lte=130"`
}
```

**Validation Errors:**
Validation failures MUST be transformed to `CodeInvalidArgument` errors with field-level details.

### 8.2 Disabling Validation

Validation can be disabled per-handler:

```go
// Per-handler
service.Exec("Method", fn, tygor.WithoutValidation())
```

---

## 9. Internal Architecture

### 9.1 Internal Handler Interface

The framework MUST keep its handler interface internal. Public registration is
available only through the typed `Service` methods.

```go
package tygor

type endpointHandler interface {
    serveHTTP(ctx *rpcContext)
    metadata() *meta.MethodMetadata
}
```

---

## 10. Interceptors

### 10.1 Interceptor Signature

The library MUST support interceptors (middleware) with the following signature:

```go
type UnaryInterceptor func(ctx Context, req any, handler HandlerFunc) (res any, err error)

type HandlerFunc func(ctx context.Context, req any) (res any, err error)
```

The `Context` interface provides type-safe access to RPC metadata:

```go
type Context interface {
    context.Context  // embeds standard context
    Service() string              // service name
    EndpointID() string           // full endpoint ID (e.g., "Users.Create")
    HTTPRequest() *http.Request   // underlying HTTP request
    HTTPWriter() http.ResponseWriter // response writer
}
```

### 10.2 Interceptor Scopes

Interceptors can be registered at three levels (executed in order):

1. **App-level (global):** Applied to all operations
2. **Service-level:** Applied to all operations in a service
3. **Handler-level:** Applied to a specific operation

**Example:**
```go
// Logging interceptor
func LoggingInterceptor(ctx tygor.Context, req any, handler tygor.HandlerFunc) (any, error) {
    start := time.Now()
    res, err := handler(ctx, req)
    log.Printf("%s took %v", ctx.EndpointID(), time.Since(start))
    return res, err
}

app := tygor.NewApp(tygor.WithUnaryInterceptors(LoggingInterceptor))
```

### 10.3 Execution Order

Interceptors MUST execute in the following order:
1. App interceptors (first registered → last registered)
2. Service interceptors (first registered → last registered)
3. Handler interceptors (first registered → last registered)
4. Actual handler
5. Interceptors return in reverse order (unwinding stack)

---

## 11. Context API

### 11.1 Context Type

The library provides a `Context` interface that embeds `context.Context` and provides type-safe access to RPC metadata:

```go
func FromContext(ctx context.Context) (Context, bool)
```

**In interceptors:** Receive `Context` directly with full access to RPC metadata.

**In handlers:** Use `FromContext` to extract the context:

```go
func MyHandler(ctx context.Context, req *MyRequest) (*MyResponse, error) {
    tc, ok := tygor.FromContext(ctx)
    if ok {
        userAgent := tc.HTTPRequest().Header.Get("User-Agent")
        tc.HTTPWriter().Header().Set("X-Custom-Header", "value")
    }
    return &MyResponse{}, nil
}
```

### 11.2 Context Methods

- `Service() string` - Returns the service name
- `Method() string` - Returns the method name
- `HTTPRequest() *http.Request` - Returns the underlying HTTP request
- `HTTPWriter() http.ResponseWriter` - Returns the response writer for setting headers

---

## 12. Type Generation

### 12.1 Generator API

The library MUST provide a standalone `Generate` function in the `tygorgen` package:

```go
func Generate(app *tygor.App, config *Config) error
```

This function:
- Takes the app to extract registered routes
- Uses the provided configuration to control type generation
- Generates TypeScript types and manifest files

### 12.2 Generation Configuration

The `tygorgen.Config` struct controls TypeScript type generation:

```go
type Config struct {
    // REQUIRED: Output directory for generated files
    OutDir string

    // OPTIONAL: Custom Go-to-TypeScript type mappings
    TypeMappings map[string]string

    // OPTIONAL: Comment preservation ("default", "types", "none")
    PreserveComments string

    // OPTIONAL: Enum generation style ("union", "enum", "const")
    EnumStyle string

    // OPTIONAL: Optional/nullable field typing ("default", "undefined", "null")
    OptionalType string

    // OPTIONAL: Custom frontmatter for generated files
    Frontmatter string
}
```

**Defaults:**
```go
{
    PreserveComments: "default",
    EnumStyle: "union",
    OptionalType: "default",
    TypeMappings: map[string]string{
        "time.Time":          "string",
        "pgtype.Timestamptz": "string | null",
        "pgtype.Text":        "string",
        "pgtype.Int4":        "number",
    },
}
```

### 12.3 Generated Files

The generator MUST produce:

1. **`types.ts`**: TypeScript interfaces for all request/response types
2. **`manifest.ts`**: Operation manifest (see TYPESCRIPT-CLIENT.md)

**Example:**
```go
_, err := tygorgen.FromApp(app).
    TypeMapping("uuid.UUID", "string").
    ToDir("./client/src/rpc")
```

---

## 13. Logging

### 13.1 Logger Interface

The library SHOULD support a logger interface compatible with `log/slog`:

```go
type Logger interface {
    Info(msg string, args ...any)
    Error(msg string, args ...any)
    Debug(msg string, args ...any)
}
```

### 13.2 Logger Configuration

```go
app := tygor.NewApp(tygor.WithLogger(slog.Default()))
```

**Logged Events:**
- Request start/end
- Errors (with error details)
- Panics (recovered)

---

## 14. Complete Example

```go
package main

import (
    "context"
    "log"
    "log/slog"
    "net/http"
    "time"

    "tygor.dev"
    "tygor.dev/tygorgen"
    "myapp/internal/db"
)

var dbPool *pgxpool.Pool // initialized elsewhere

// Handler functions
func ListNews(ctx context.Context, req *db.ListNewsParams) ([]*db.News, error) {
    queries := db.New(dbPool)
    return queries.ListNews(ctx, req)
}

func CreateNews(ctx context.Context, req *db.CreateNewsParams) (*db.News, error) {
    queries := db.New(dbPool)
    return queries.CreateNews(ctx, req)
}

func main() {
    // Create app
    app := tygor.NewApp(
        tygor.WithLogger(slog.Default()),
        tygor.WithMaskInternalErrors(),
    )

    // Register services
    news := app.Service("News")
    news.Query("List", ListNews, tygor.WithCacheControl(tygor.CacheConfig{
        MaxAge: 5 * time.Minute,
        Public: true,
    }))
    news.Exec("Create", CreateNews)

    // Generate TypeScript types
    if _, err := tygorgen.FromApp(app).ToDir("./client/src/rpc"); err != nil {
        log.Fatal(err)
    }

    // Start server
    log.Fatal(http.ListenAndServe(":8080", app.Handler()))
}
```

---

## 15. Compliance Checklist

A conforming Go implementation MUST:

- ✅ Support `func(ctx context.Context, req Req) (Res, error)` handlers
- ✅ Provide typed `Service.Exec()`, `Service.Query()`, `Service.Stream()`, and `Service.LiveValue()` registration with capability-specific options
- ✅ Provide `App` with `Service` namespacing
- ✅ Provide `Handler()` method returning `http.Handler`
- ✅ Support GET (query string) and POST (JSON body) serialization
- ✅ Implement default error transformer with standard error codes
- ✅ Support custom error transformers
- ✅ Keep the handler interface internal
- ✅ Support interceptors at app/service/handler levels via `WithUnaryInterceptors`
- ✅ Provide context API for request metadata (`FromContext`, `Context` interface with `Service()`, `EndpointID()`, `HTTPRequest()`, `HTTPWriter()`)
- ✅ Generate TypeScript types and manifest
