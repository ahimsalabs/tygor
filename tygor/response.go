package tygor

import json "encoding/json/v2"

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
type Empty = *struct{}

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

// marshalResponse serializes the complete success envelope before any response
// headers or body bytes are committed.
func marshalResponse(result any, options ...json.Options) ([]byte, error) {
	payload, err := json.Marshal(result, options...)
	if err != nil {
		return nil, err
	}
	data := make([]byte, 0, len(payload)+13)
	data = append(data, `{"result":`...)
	data = append(data, payload...)
	return append(data, '}', '\n'), nil
}

func marshalErrorResponse(err *Error) ([]byte, error) {
	data, marshalErr := json.Marshal(errorResponse{Error: err})
	if marshalErr != nil {
		return nil, marshalErr
	}
	return append(data, '\n'), nil
}
