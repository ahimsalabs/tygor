// Package tygorgen provides example types for tygorgen documentation.
package tygorgen

import "time"

// [snippet:user-type]
type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name" validate:"required,min=2"`
	Email     string    `json:"email" validate:"required,email"`
	Avatar    *string   `json:"avatar"` // nullable
	CreatedAt time.Time `json:"created_at"`
}

// [/snippet:user-type]
