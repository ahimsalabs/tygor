// Package v2 contains version 2 types.
package v2

// User is the v2 user type with additional fields.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}
