package testdata

// Test type alias chains to verify no infinite recursion in convertType.
// Go's compiler prevents directly recursive aliases like "type A = A",
// but alias chains can still occur.

// Simple alias chain: A -> B -> underlying
type AliasLevel1 = string
type AliasLevel2 = AliasLevel1
type AliasLevel3 = AliasLevel2

// Alias to named type
type AliasToNamed = User

// Alias chain to struct
type BaseStruct struct {
	Value string `json:"value"`
}
type AliasToStruct = BaseStruct
type AliasToAliasStruct = AliasToStruct

// Container using alias types
type AliasContainer struct {
	Level1 AliasLevel1        `json:"level1"`
	Level2 AliasLevel2        `json:"level2"`
	Level3 AliasLevel3        `json:"level3"`
	Named  AliasToNamed       `json:"named"`
	Struct AliasToStruct      `json:"struct"`
	Deep   AliasToAliasStruct `json:"deep"`
}

// Self-referential struct via alias (tests that Named type breaks cycle)
type NodeAlias = Node
type Node struct {
	Value string     `json:"value"`
	Next  *NodeAlias `json:"next,omitempty"`
}

// Generic alias (Go 1.24+)
type GenericAlias[T any] = []T
type StringSliceAlias = GenericAlias[string]

// Alias of pointer to alias
type PtrToAlias = *AliasLevel1

// Map with alias types
type AliasMap = map[AliasLevel1]AliasToStruct
