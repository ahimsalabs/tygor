package tygor

import "encoding/json"

// Empty represents a void request or response.
// Use this for operations that don't return meaningful data.
// The zero value is nil, which serializes to JSON null.
//
// Example:
//
//	func DeleteUser(ctx context.Context, req *DeleteUserRequest) (tygor.Empty, error) {
//	    // ... delete user
//	    return nil, nil
//	}
//
// Wire format: {"result": null}
type Empty *struct{}

// response is the internal envelope type for successful responses.
// This wraps the actual result in a {"result": ...} structure.
type response struct {
	Result any `json:"result"`
}

// errorResponse is the internal envelope type for error responses.
// This wraps the error in an {"error": {...}} structure.
type errorResponse struct {
	Error *Error `json:"error"`
}

// encodeResponse writes a successful response to the ResponseWriter.
func encodeResponse(w jsonWriter, result any) error {
	return json.NewEncoder(w).Encode(response{Result: result})
}

// encodeErrorResponse writes an error response to the ResponseWriter.
func encodeErrorResponse(w jsonWriter, err *Error) error {
	return json.NewEncoder(w).Encode(errorResponse{Error: err})
}

// jsonWriter is satisfied by http.ResponseWriter and allows testing.
type jsonWriter interface {
	Write([]byte) (int, error)
}
