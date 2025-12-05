# tygorgen

Go to TypeScript code generator. Generates TypeScript types and Zod schemas from Go structs.

## Features

- **Zod schema generation** from Go `validate` tags
- **Nullable vs optional** correctly distinguished (`*T` vs `omitempty`)
- **Enum support** from Go const groups with iota
- **Doc comments** preserved in TypeScript output
- **Custom type mappings** for `time.Time`, `uuid.UUID`, etc.

## Usage

### With tygor app

<!-- [snippet:doc/examples/tygorgen:from-app] -->
```go title="main.go"
tygorgen.FromApp(app).
	WithFlavor(tygorgen.FlavorZod).
	ToDir("./client/src/rpc")
```
<!-- [/snippet:doc/examples/tygorgen:from-app] -->

### Standalone type generation

Generate TypeScript from Go types without the full RPC framework:

<!-- [snippet:doc/examples/tygorgen:from-types] -->
```go title="main.go"
tygorgen.FromTypes(
	User{},
	CreateUserRequest{},
	ListUsersResponse{},
).ToDir("./client/src/types")
```
<!-- [/snippet:doc/examples/tygorgen:from-types] -->

### With Zod schemas

<!-- [snippet:doc/examples/tygorgen:from-types-zod] -->
```go title="main.go"
tygorgen.FromTypes(User{}).
	WithFlavor(tygorgen.FlavorZod).
	ToDir("./client/src/types")
```
<!-- [/snippet:doc/examples/tygorgen:from-types-zod] -->

## Example

**Go input:**
<!-- [snippet:doc/examples/tygorgen:user-type] -->
```go title="types.go"
type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name" validate:"required,min=2"`
	Email     string    `json:"email" validate:"required,email"`
	Avatar    *string   `json:"avatar"` // nullable
	CreatedAt time.Time `json:"created_at"`
}

```
<!-- [/snippet:doc/examples/tygorgen:user-type] -->

**TypeScript output:**
<!-- [snippet:doc/examples/tygorgen:ts-output] -->
```typescript title="generated_types.ts"
export interface User {
  id: number;
  name: string;
  email: string;
  avatar: string | null;
  created_at: string;
}
```
<!-- [/snippet:doc/examples/tygorgen:ts-output] -->

**Zod output:**
<!-- [snippet:doc/examples/tygorgen:zod-output] -->
```typescript title="generated_schemas.ts"
export const UserSchema = z.object({
  id: z.number().int(),
  name: z.string().min(1).min(2),
  email: z.string().min(1).email(),
  avatar: z.nullable(z.string()),
  created_at: z.string().datetime(),
});
```
<!-- [/snippet:doc/examples/tygorgen:zod-output] -->

## Configuration

<!-- [snippet:doc/examples/tygorgen:config] -->
```go title="main.go"
tygorgen.FromApp(app).
	WithFlavor(tygorgen.FlavorZod).     // Generate Zod schemas
	WithFlavor(tygorgen.FlavorZodMini). // Or use zod/mini for smaller bundles
	SingleFile().                       // All types in one file
	EnumStyle("enum").                  // "union" | "enum" | "const"
	OptionalType("null").               // "undefined" | "null"
	TypeMapping("time.Time", "Date").   // Custom type mappings
	PreserveComments("types").          // "default" | "types" | "none"
	ToDir("./client/src/rpc")
```
<!-- [/snippet:doc/examples/tygorgen:config] -->

## Providers

Two extraction strategies are available:

| Provider | Speed | Enums | Comments | Use case |
|----------|-------|-------|----------|----------|
| `source` (default) | Slower | Yes | Yes | Production builds |
| `reflection` | Fast | No | No | Development iteration |

<!-- [snippet:doc/examples/tygorgen:provider] -->
```go title="main.go"
tygorgen.FromTypes(User{}).
	Provider("reflection"). // Fast mode
	ToDir("./client/src/types")
```
<!-- [/snippet:doc/examples/tygorgen:provider] -->

## See also

Other Go to TypeScript generators:

- [tygo](https://github.com/gzuidhof/tygo) - CLI with YAML config, enums, comments
- [guts](https://github.com/coder/guts) - Library using TypeScript compiler API
- [typescriptify-golang-structs](https://github.com/tkrajina/typescriptify-golang-structs) - Library with enum support
- [go2ts](https://github.com/StirlingMarketingGroup/go2ts) - Online converter tool
