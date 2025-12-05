<!-- snippet-lint-disable -->

# Validation in Tygor

Tygor provides multiple layers of validation for request handling, allowing you to choose the right approach for your needs.

## Built-in Struct Validation

By default, all handlers validate requests using the [go-playground/validator](https://github.com/go-playground/validator) package.

```go
type CreateUserRequest struct {
    Username string `json:"username" validate:"required,min=3,max=32"`
    Email    string `json:"email" validate:"required,email"`
    Age      int    `json:"age" validate:"gte=0,lte=130"`
}

func CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) {
    // Request is already validated when this function is called
    // ...
}
```

Validation happens automatically before your handler is called. If validation fails, an `invalid_argument` error is returned to the client.

## Query Parameter Validation

### Lenient Mode (Default)

By default, GET requests ignore unknown query parameters. This provides flexibility for clients and forward compatibility:

```go
// Unknown params like ?typo=value or ?analytics_id=123 are silently ignored
Query(ListUsers)
```

### Strict Mode

For handlers where you want to catch parameter typos or enforce exact expectations:

```go
type SearchParams struct {
    Query string `schema:"query" validate:"required"`
    Limit int    `schema:"limit" validate:"min=1,max=100"`
}

// Returns error if client sends unknown query parameters
Query(SearchUsers).WithStrictQueryParams()
```

This helps during development to catch mistakes like `?usre_id=123` instead of `?user_id=123`.

## Skipping Built-in Validation

If you need to handle validation manually or the request type has no validation tags:

```go
type BulkRequest struct {
    Operations []Operation `json:"operations"`
}

func BulkUpdate(ctx context.Context, req *BulkRequest) (*BulkResponse, error) {
    // Validate manually
    if len(req.Operations) == 0 {
        return nil, tygor.Errorf(tygor.CodeInvalidArgument, "operations cannot be empty")
    }
    // ...
}

Exec(BulkUpdate).WithSkipValidation()
```

## Custom Validation with Interceptors

For complex validation logic, business rules, or cross-field validation, use interceptors:

```go
func CustomValidationInterceptor(ctx *tygor.Context, req any, handler tygor.HandlerFunc) (any, error) {
    // Type assert to your specific request type
    if searchReq, ok := req.(*SearchRequest); ok {
        // Custom validation logic
        if searchReq.Query == "" {
            return nil, tygor.Errorf(tygor.CodeInvalidArgument, "query cannot be empty")
        }

        if len(searchReq.Query) > 1000 {
            return nil, tygor.Errorf(tygor.CodeInvalidArgument, "query too long")
        }

        // Business logic validation
        if searchReq.Limit > 100 {
            return nil, tygor.Errorf(tygor.CodeInvalidArgument, "limit cannot exceed 100")
        }
    }

    // Continue the chain
    return handler(ctx, req)
}

// Apply globally
app.WithUnaryInterceptor(CustomValidationInterceptor)

// Or per-handler
Query(Search).WithUnaryInterceptor(CustomValidationInterceptor)
```

Interceptors are useful for:
- Complex cross-field validation
- Business logic validation (e.g., checking database state)
- Custom error messages
- Different validation rules per handler
- Validation that depends on context (user permissions, feature flags, etc.)

## Validation Order

When a request arrives, validation happens in this order:

1. **Query/Body Decoding**: Parameters are decoded into the request struct
   - For GET requests with `WithStrictQueryParams()`, unknown parameters cause an error here
2. **Struct Validation**: The `validator` package validates struct tags (unless `WithSkipValidation()` is used)
3. **Interceptors**: Global, service, and handler-level interceptors run (can include custom validation)
4. **Handler Function**: Your handler executes with a validated request

## Choosing the Right Approach

| Use Case | Approach |
|----------|----------|
| Standard constraints (required, min, max, email) | Struct tags with `validate:` |
| Catch query parameter typos | `WithStrictQueryParams()` |
| Complex cross-field validation | Interceptor |
| Business logic validation | Interceptor |
| Manual validation | `WithSkipValidation()` + validate in handler |
| Different rules per handler | Handler-level interceptor |
| Database lookups for validation | Interceptor |

## Error Codes

Validation errors should use the `CodeInvalidArgument` error code:

```go
return nil, tygor.Errorf(tygor.CodeInvalidArgument, "limit must be between 1 and 100")
```

This maps to HTTP 400 Bad Request and signals to clients that they need to fix their input.
