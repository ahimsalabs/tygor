package shadow

import (
	"context"
	"encoding/json"
)

type int string

func EchoHandler() func(context.Context, int) (int, error) {
	return func(_ context.Context, value int) (int, error) {
		return value, nil
	}
}

func JSONValue() ([]byte, error) {
	return json.Marshal(int("value"))
}
