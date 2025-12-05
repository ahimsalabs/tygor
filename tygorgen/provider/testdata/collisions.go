package testdata

// CollisionTestA is the first type with this base name.
type CollisionTestA struct {
	Field string
}

// CollisionTestB is the second type - no collision yet.
type CollisionTestB struct {
	Field int
}

// The following would cause a collision if both tried to be added:

// DuplicateType is a type that exists.
type DuplicateType struct {
	Value string
}

// SamePackageDuplicate demonstrates same-package collision.
// This should not be extracted at the same time as DuplicateType
// in tests that check for collisions.
type SamePackageDuplicate struct {
	ID int
}
