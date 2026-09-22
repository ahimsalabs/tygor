package tygor

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type jsonOptionNumber struct {
	Value int `json:"value"`
}

func TestJSONOptionsScopePrecedenceAndExecCodec(t *testing.T) {
	app := NewApp(WithJSONOptions(json.StringifyNumbers(true)))
	service := app.Service("Numbers", WithJSONOptions(json.StringifyNumbers(false)))
	service.Exec("Service", func(_ context.Context, req jsonOptionNumber) (jsonOptionNumber, error) {
		return req, nil
	})
	service.Exec("Endpoint", func(_ context.Context, req jsonOptionNumber) (jsonOptionNumber, error) {
		return req, nil
	}, WithJSONOptions(json.StringifyNumbers(true)))
	service.Exec("ExplicitFalse", func(_ context.Context, req jsonOptionNumber) (jsonOptionNumber, error) {
		return req, nil
	}, WithJSONOptions(json.StringifyNumbers(true)), WithJSONOptions(json.StringifyNumbers(false)))

	tests := []struct {
		path string
		body string
		want string
	}{
		{"/Numbers/Service", `{"value":2}`, "{\"result\":{\"value\":2}}\n"},
		{"/Numbers/Endpoint", `{"value":"3"}`, "{\"result\":{\"value\":\"3\"}}\n"},
		{"/Numbers/ExplicitFalse", `{"value":4}`, "{\"result\":{\"value\":4}}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			app.ServeHTTP(w, req)
			if w.Code != http.StatusOK || w.Body.String() != tt.want {
				t.Fatalf("response = %d %q, want 200 %q", w.Code, w.Body.String(), tt.want)
			}
		})
	}
}

func TestJSONOptionsAppFalseServiceTrueEndpointFalse(t *testing.T) {
	app := NewApp(WithJSONOptions(json.StringifyNumbers(false)))
	service := app.Service("Numbers", WithJSONOptions(json.StringifyNumbers(true)))
	service.Exec("Endpoint", func(_ context.Context, req jsonOptionNumber) (jsonOptionNumber, error) {
		return req, nil
	}, WithJSONOptions(json.StringifyNumbers(false)))

	req := httptest.NewRequest(http.MethodPost, "/Numbers/Endpoint", strings.NewReader(`{"value":5}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if got, want := w.Body.String(), "{\"result\":{\"value\":5}}\n"; got != want {
		t.Fatalf("body = %q, want endpoint explicit false %q", got, want)
	}
}

func TestJSONCodecRegistryReplacementClearingAndMetadata(t *testing.T) {
	registryA := json.MarshalFunc(func(jsonOptionNumber) ([]byte, error) { return []byte(`"app"`), nil })
	registryB := json.MarshalFunc(func(jsonOptionNumber) ([]byte, error) { return []byte(`"service"`), nil })
	app := NewApp(WithJSONOptions(json.WithMarshalers(registryA)))
	service := app.Service("Codec", WithJSONOptions(json.WithMarshalers(registryB)))
	service.Query("Service", func(context.Context, jsonOptionNumber) (jsonOptionNumber, error) {
		return jsonOptionNumber{Value: 1}, nil
	})
	service.Query("Cleared", func(context.Context, jsonOptionNumber) (jsonOptionNumber, error) {
		return jsonOptionNumber{Value: 2}, nil
	}, WithJSONOptions(json.WithMarshalers(nil)))

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/Codec/Service", want: "{\"result\":\"service\"}\n"},
		{path: "/Codec/Cleared", want: "{\"result\":{\"value\":2}}\n"},
	} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, test.path, nil))
		if got := w.Body.String(); got != test.want {
			t.Fatalf("%s body = %q, want %q", test.path, got, test.want)
		}
	}

	routes := app.Routes()
	serviceRegistry, _ := json.GetOption(routes["Codec.Service"].JSONOptions, json.WithMarshalers)
	if serviceRegistry != registryB {
		t.Fatal("service registry did not replace app registry in endpoint metadata")
	}
	clearedRegistry, _ := json.GetOption(routes["Codec.Cleared"].JSONOptions, json.WithMarshalers)
	if clearedRegistry != nil {
		t.Fatal("endpoint nil registry did not clear inherited registry in metadata")
	}
}

func TestJSONOptionsAreSnapshottedAtRegistration(t *testing.T) {
	app := NewApp()
	service := app.Service("Snapshot")
	handler := func(context.Context, jsonOptionNumber) (jsonOptionNumber, error) {
		return jsonOptionNumber{Value: 7}, nil
	}
	service.Query("Before", handler)
	jsonOption{options: json.StringifyNumbers(true)}.applyApp(app)
	service.Query("After", handler)

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/Snapshot/Before", want: "{\"result\":{\"value\":7}}\n"},
		{path: "/Snapshot/After", want: "{\"result\":{\"value\":\"7\"}}\n"},
	} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, test.path, nil))
		if got := w.Body.String(); got != test.want {
			t.Fatalf("%s body = %q, want %q", test.path, got, test.want)
		}
	}

	routes := app.Routes()
	before, _ := json.GetOption(routes["Snapshot.Before"].JSONOptions, json.StringifyNumbers)
	after, _ := json.GetOption(routes["Snapshot.After"].JSONOptions, json.StringifyNumbers)
	if before || !after {
		t.Fatalf("metadata snapshots = before %t, after %t", before, after)
	}
}

func TestQueryUsesSchemaInputAndJSONOptionsForResponse(t *testing.T) {
	app := NewApp()
	app.Service("Numbers").Query("Get", func(_ context.Context, req jsonOptionNumber) (jsonOptionNumber, error) {
		return req, nil
	}, WithJSONOptions(json.StringifyNumbers(true)))
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Numbers/Get?value=42", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"result":{"value":"42"}}`+"\n" {
		t.Fatalf("response = %d %q, want Gorilla Schema input and stringified JSON output", w.Code, w.Body.String())
	}
}

func TestJSONFormattingDoesNotReformatFrameworkEnvelopes(t *testing.T) {
	options := WithJSONOptions(jsontext.WithIndent("  "))
	app := NewApp(options)
	app.Service("Values").Exec("Get", func(context.Context, Empty) (jsonOptionNumber, error) {
		return jsonOptionNumber{Value: 7}, nil
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/Values/Get", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, req)
	want := "{\"result\":{\n  \"value\": 7\n}}\n"
	if w.Body.String() != want {
		t.Fatalf("body = %q, want stable outer envelope %q", w.Body.String(), want)
	}
}

func TestStreamJSONOptionsAndMultilineSSEFraming(t *testing.T) {
	app := NewApp(WithStreamWriteTimeout(0), WithStreamHeartbeat(0))
	app.Service("Feed").Stream("Get", func(_ context.Context, _ Empty, stream StreamWriter[jsonOptionNumber]) error {
		return stream.Send(jsonOptionNumber{Value: 8})
	}, WithJSONOptions(json.StringifyNumbers(true), jsontext.WithIndent("  ")))
	w := newStreamRecorder()
	req := httptest.NewRequest(http.MethodPost, "/Feed/Get", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, req)
	want := "data: {\"result\":{\n" +
		"data:   \"value\": \"8\"\n" +
		"data: }}\n\n"
	if w.Body.String() != want {
		t.Fatalf("SSE body = %q, want %q", w.Body.String(), want)
	}
}

func readLiveValueInitial(t *testing.T, app *App, path string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &liveValueObservedWriter{
		ResponseRecorder: httptest.NewRecorder(),
		onFirstWrite:     cancel,
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, req)
	return w.Body.String()
}

func TestSharedLiveValueHasEndpointScopedJSONProjections(t *testing.T) {
	live := mustNewLiveValue(t, jsonOptionNumber{Value: 9})
	app := NewApp(WithStreamWriteTimeout(0), WithStreamHeartbeat(0))
	service := app.Service("State")
	service.LiveValue("Number", live)
	service.LiveValue("String", live, WithJSONOptions(json.StringifyNumbers(true)))

	if got, want := readLiveValueInitial(t, app, "/State/Number"), "data: {\"result\":{\"value\":9}}\n\n"; got != want {
		t.Fatalf("numeric projection = %q, want %q", got, want)
	}
	if got, want := readLiveValueInitial(t, app, "/State/String"), "data: {\"result\":{\"value\":\"9\"}}\n\n"; got != want {
		t.Fatalf("string projection = %q, want %q", got, want)
	}
}

type failNthLiveValue struct {
	Value int `json:"value"`
}

var failNthLiveValueMarshalCalls atomic.Int32

func (value failNthLiveValue) MarshalJSON() ([]byte, error) {
	if failNthLiveValueMarshalCalls.Add(1) == 4 {
		return nil, errors.New("projection failed")
	}
	return json.Marshal(struct {
		Value int `json:"value"`
	}{value.Value})
}

type failSecondWriteRecorder struct {
	*streamRecorder
	writes int
}

func (w *failSecondWriteRecorder) Write(data []byte) (int, error) {
	w.writes++
	if w.writes == 2 {
		return 0, errors.New("terminal frame failed")
	}
	return w.streamRecorder.Write(data)
}

func TestLiveValueSerializationFailureTerminalWriteFailureAborts(t *testing.T) {
	failNthLiveValueMarshalCalls.Store(0)
	live := mustNewLiveValue(t, failNthLiveValue{Value: 1})
	app := NewApp(WithStreamWriteTimeout(0), WithStreamHeartbeat(0))
	app.Service("State").LiveValue("Get", live)
	w := &failSecondWriteRecorder{streamRecorder: newStreamRecorder()}
	req := httptest.NewRequest(http.MethodPost, "/State/Get", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		app.ServeHTTP(w, req)
	}()
	for failNthLiveValueMarshalCalls.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	if err := live.Set(failNthLiveValue{Value: 2}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	select {
	case recovered := <-done:
		if recovered == nil {
			t.Fatal("ServeHTTP did not abort transport after terminal frame write failure")
		}
	case <-time.After(time.Second):
		t.Fatal("ServeHTTP did not return")
	}
	if w.writes != 2 {
		t.Fatalf("writes = %d, want initial frame and one failed terminal frame", w.writes)
	}
}
