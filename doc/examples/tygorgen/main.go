// Package tygorgen provides example usage for tygorgen documentation.
package tygorgen

import (
	"tygor.dev/tygor"
	"tygor.dev/tygorgen"
)

func exampleFromApp() {
	app := tygor.NewApp()
	// [snippet:from-app]
	tygorgen.FromApp(app).
		WithFlavor(tygorgen.FlavorZod).
		ToDir("./client/src/rpc")
	// [/snippet:from-app]
}

func exampleFromTypes() {
	// [snippet:from-types]
	tygorgen.FromTypes(
		User{},
		CreateUserRequest{},
		ListUsersResponse{},
	).ToDir("./client/src/types")
	// [/snippet:from-types]
}

func exampleFromTypesZod() {
	// [snippet:from-types-zod]
	tygorgen.FromTypes(User{}).
		WithFlavor(tygorgen.FlavorZod).
		ToDir("./client/src/types")
	// [/snippet:from-types-zod]
}

func exampleConfig() {
	app := tygor.NewApp()
	// [snippet:config]
	tygorgen.FromApp(app).
		WithFlavor(tygorgen.FlavorZod).     // Generate Zod schemas
		WithFlavor(tygorgen.FlavorZodMini). // Or use zod/mini for smaller bundles
		SingleFile().                       // All types in one file
		EnumStyle("enum").                  // "union" | "enum" | "const"
		OptionalType("null").               // "undefined" | "null"
		TypeMapping("time.Time", "Date").   // Custom type mappings
		PreserveComments("types").          // "default" | "types" | "none"
		ToDir("./client/src/rpc")
	// [/snippet:config]
}

func exampleProvider() {
	// [snippet:provider]
	tygorgen.FromTypes(User{}).
		Provider("reflection"). // Fast mode
		ToDir("./client/src/types")
	// [/snippet:provider]
}

// Types used by snippets
type CreateUserRequest struct{}
type ListUsersResponse struct{}

// Keep imports used
var (
	_ = exampleFromApp
	_ = exampleFromTypes
	_ = exampleFromTypesZod
	_ = exampleConfig
	_ = exampleProvider
)
