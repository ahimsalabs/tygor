// Package testfixtures provides types used for testing the tygorgen package.
package testfixtures

// CreateUserRequest is a test fixture for generator tests.
type CreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// User is a test fixture for generator tests.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// ListPostsParams is a test fixture for generator tests.
type ListPostsParams struct {
	AuthorID  *int64 `json:"author_id" schema:"author_id"`
	Published *bool  `json:"published" schema:"published"`
	Limit     int32  `json:"limit" schema:"limit"`
	Offset    int32  `json:"offset" schema:"offset"`
}

// Post is a test fixture for generator tests.
type Post struct {
	ID        int64  `json:"id"`
	AuthorID  int64  `json:"author_id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Published bool   `json:"published"`
}
