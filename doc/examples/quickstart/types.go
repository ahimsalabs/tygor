// Package quickstart provides simple example types for documentation.
package quickstart

// [snippet:types]
type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email" validate:"required,email"`
}

type GetUserRequest struct {
	ID int64 `json:"id"`
}

type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

// [/snippet:types]
