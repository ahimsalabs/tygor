package tygor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type StreamRequest struct {
	Topic string `json:"topic" validate:"required"`
}

type StreamEvent struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

func TestStream_Metadata(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {}
	}

	handler := streamIter2(fn)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}

	meta := handler.Metadata()
	if meta.Primitive != "stream" {
		t.Errorf("expected Primitive stream, got %s", meta.Primitive)
	}
}

func TestStream_BasicEvents(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {
			for i := 1; i <= 3; i++ {
				if !yield(StreamEvent{ID: i, Message: "event"}, nil) {
					return
				}
			}
		}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", contentType)
	}

	// Parse SSE events
	events := parseSSEEvents(t, w.Body.String())
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	for i, event := range events {
		if event.Result.ID != i+1 {
			t.Errorf("event %d: expected ID %d, got %d", i, i+1, event.Result.ID)
		}
	}
}

func TestStream_ErrorMidStream(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {
			if !yield(StreamEvent{ID: 1, Message: "ok"}, nil) {
				return
			}
			yield(StreamEvent{}, errors.New("database connection lost"))
		}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should still be 200 (headers sent before error)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Parse events - should have 1 success + 1 error
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %s", len(lines), w.Body.String())
	}

	// Last event should be an error
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, `"error"`) {
		t.Errorf("expected error in last event, got: %s", lastLine)
	}
}

func TestStream_ValidationError(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn))

	// Missing required "topic" field
	body := `{}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Validation error should return before streaming starts
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStream_UnaryInterceptor_Reject(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {
			yield(StreamEvent{ID: 1}, nil)
		}
	}

	authInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return nil, NewError(CodeUnauthenticated, "not logged in")
	}

	app := NewApp().WithUnaryInterceptor(authInterceptor)
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should reject before streaming
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStream_StreamInterceptor(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {
			for i := 1; i <= 3; i++ {
				if !yield(StreamEvent{ID: i, Message: "original"}, nil) {
					return
				}
			}
		}
	}

	// Interceptor that transforms events
	transformInterceptor := func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error] {
		events := handler(ctx, req)
		return func(yield func(any, error) bool) {
			for event, err := range events {
				if err != nil {
					yield(nil, err)
					return
				}
				// Transform the event
				e := event.(StreamEvent)
				e.Message = "transformed"
				if !yield(e, nil) {
					return
				}
			}
		}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn).WithStreamInterceptor(transformInterceptor))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	events := parseSSEEvents(t, w.Body.String())
	for _, event := range events {
		if event.Result.Message != "transformed" {
			t.Errorf("expected transformed message, got: %s", event.Result.Message)
		}
	}
}

func TestStream_ClientDisconnect(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})

	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {
			close(started)
			// Wait for context cancellation
			<-ctx.Done()
			close(done)
		}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn))

	server := httptest.NewServer(app.Handler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	reqBody := strings.NewReader(`{"topic":"news"}`)
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/Feed/Subscribe", reqBody)
	req.Header.Set("Content-Type", "application/json")

	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait for handler to start
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler didn't start")
	}

	// Cancel the request (simulate client disconnect)
	cancel()

	// Handler should detect disconnection
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler didn't detect client disconnect")
	}
}

func TestStream_MethodNotAllowed(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn))

	// Try GET instead of POST
	req := httptest.NewRequest("GET", "/Feed/Subscribe?topic=news", nil)
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStream_WithSkipValidation(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {
			yield(StreamEvent{ID: 1, Message: "ok"}, nil)
		}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn).WithSkipValidation())

	// Missing required field, but validation is skipped
	body := `{}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

// Helper to parse SSE events from response body
type sseEventEnvelope struct {
	Result StreamEvent `json:"result"`
	Error  *Error      `json:"error"`
}

func parseSSEEvents(t *testing.T, body string) []sseEventEnvelope {
	t.Helper()
	var events []sseEventEnvelope

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var env sseEventEnvelope
			if err := json.Unmarshal([]byte(data), &env); err != nil {
				t.Fatalf("failed to parse SSE event: %v\ndata: %s", err, data)
			}
			if env.Error == nil { // Only collect success events
				events = append(events, env)
			}
		}
	}
	return events
}

// Silence unused variable warning
var _ = bytes.Buffer{}

// =============================================================================
// Stream (Emitter-based) tests
// =============================================================================

func TestStreamEmit_BasicEvents(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		for i := 1; i <= 3; i++ {
			if err := e.Send(StreamEvent{ID: i, Message: "event"}); err != nil {
				return err
			}
		}
		return nil
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	events := parseSSEEvents(t, w.Body.String())
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	for i, event := range events {
		if event.Result.ID != i+1 {
			t.Errorf("event %d: expected ID %d, got %d", i, i+1, event.Result.ID)
		}
	}
}

func TestStreamEmit_HandlerError(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		if err := e.Send(StreamEvent{ID: 1, Message: "ok"}); err != nil {
			return err
		}
		return errors.New("database connection lost")
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should still be 200 (headers sent before error)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Parse events - should have 1 success + 1 error
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %s", len(lines), w.Body.String())
	}

	// Last event should be an error
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, `"error"`) {
		t.Errorf("expected error in last event, got: %s", lastLine)
	}
}

func TestStreamEmit_ErrStreamClosedNotSentToClient(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		if err := e.Send(StreamEvent{ID: 1}); err != nil {
			return err
		}
		// Simulate Send returning ErrStreamClosed (client disconnected)
		// Handler returns it - should NOT be sent as error event
		return ErrStreamClosed
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Should only have 1 event, no error event
	responseBody := w.Body.String()
	if strings.Contains(responseBody, `"error"`) {
		t.Errorf("ErrStreamClosed should not be sent to client, got: %s", responseBody)
	}

	events := parseSSEEvents(t, responseBody)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestStreamEmit_ContextCancellation(t *testing.T) {
	started := make(chan struct{})
	handlerDone := make(chan struct{})

	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		close(started)
		// Wait for context cancellation
		<-ctx.Done()
		close(handlerDone)
		return nil
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	server := httptest.NewServer(app.Handler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	reqBody := strings.NewReader(`{"topic":"news"}`)
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/Feed/Subscribe", reqBody)
	req.Header.Set("Content-Type", "application/json")

	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait for handler to start
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler didn't start")
	}

	// Cancel the request
	cancel()

	// Handler should detect cancellation
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler didn't detect context cancellation")
	}
}

func TestStreamEmit_SendChecksContext(t *testing.T) {
	started := make(chan struct{})
	sendErr := make(chan error, 1)

	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		close(started)
		// Wait for context to be canceled by client disconnect
		<-ctx.Done()
		// Send should return wrapped error since request context is canceled
		err := e.Send(StreamEvent{ID: 1})
		sendErr <- err
		return err
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	server := httptest.NewServer(app.Handler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	reqBody := strings.NewReader(`{"topic":"news"}`)
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/Feed/Subscribe", reqBody)
	req.Header.Set("Content-Type", "application/json")

	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait for handler to start
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler didn't start")
	}

	// Cancel the request context
	cancel()

	select {
	case err := <-sendErr:
		// Error should match BOTH ErrStreamClosed and context.Canceled
		if !errors.Is(err, ErrStreamClosed) {
			t.Errorf("expected ErrStreamClosed, got %v", err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("didn't receive emit error")
	}
}

func TestStreamEmit_WithOptions(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		return e.Send(StreamEvent{ID: 1})
	}

	authInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		// Allow all
		return handler(ctx, req)
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn).
		WithUnaryInterceptor(authInterceptor).
		WithWriteTimeout(10*time.Second).
		WithSkipValidation())

	// Missing required field, but validation is skipped
	body := `{}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStreamEmit_LastEventID(t *testing.T) {
	var receivedLastEventID string

	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		receivedLastEventID = e.LastEventID()
		return e.Send(StreamEvent{ID: 1, Message: "ok"})
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Last-Event-ID", "42")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if receivedLastEventID != "42" {
		t.Errorf("expected LastEventID '42', got '%s'", receivedLastEventID)
	}
}

func TestStreamEmit_SendWithID(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		if err := e.SendWithID("event-1", StreamEvent{ID: 1, Message: "first"}); err != nil {
			return err
		}
		if err := e.Send(StreamEvent{ID: 2, Message: "no-id"}); err != nil {
			return err
		}
		return e.SendWithID("event-3", StreamEvent{ID: 3, Message: "third"})
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Parse the SSE response
	response := w.Body.String()

	// First event should have id: event-1
	if !strings.Contains(response, "id: event-1\n") {
		t.Errorf("expected 'id: event-1' in response, got:\n%s", response)
	}

	// Second event should NOT have an id field
	// Check that we don't have consecutive id fields (which would indicate second event has id)
	lines := strings.Split(response, "\n")
	foundSecondData := false
	for i, line := range lines {
		if strings.Contains(line, `"id":2`) { // The JSON content for second event
			foundSecondData = true
			// Check that the previous non-empty line is not "id: ..."
			for j := i - 1; j >= 0; j-- {
				if lines[j] == "" {
					continue
				}
				if strings.HasPrefix(lines[j], "id:") {
					// This would be wrong - the second event shouldn't have an id
					t.Errorf("second event should not have SSE id field")
				}
				break
			}
		}
	}
	if !foundSecondData {
		t.Errorf("could not find second event in response:\n%s", response)
	}

	// Third event should have id: event-3
	if !strings.Contains(response, "id: event-3\n") {
		t.Errorf("expected 'id: event-3' in response, got:\n%s", response)
	}
}

func TestStreamEmit_WithMaxRequestBodySize(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		return e.Send(StreamEvent{ID: 1, Message: req.Topic})
	}

	app := NewApp()
	svc := app.Service("Feed")
	// Set a very small body size limit (10 bytes)
	svc.Register("Subscribe", Stream(fn).WithMaxRequestBodySize(10))

	// Send a body larger than the limit
	body := `{"topic":"this is a very long topic that exceeds the limit"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should fail with bad request due to body size exceeded
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStreamEmit_WithHeartbeat(t *testing.T) {
	eventSent := make(chan struct{})
	handlerDone := make(chan struct{})

	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		// Send one event
		if err := e.Send(StreamEvent{ID: 1, Message: "event"}); err != nil {
			return err
		}
		close(eventSent)

		// Wait a bit for heartbeat to be sent, then finish
		time.Sleep(150 * time.Millisecond)
		close(handlerDone)
		return nil
	}

	app := NewApp()
	svc := app.Service("Feed")
	// Set heartbeat to 50ms for fast test
	svc.Register("Subscribe", Stream(fn).WithHeartbeat(50*time.Millisecond))

	server := httptest.NewServer(app.Handler())
	defer server.Close()

	reqBody := strings.NewReader(`{"topic":"news"}`)
	req, _ := http.NewRequest("POST", server.URL+"/Feed/Subscribe", reqBody)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait for handler to complete
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler didn't complete")
	}

	// Read the full response
	bodyBytes, _ := io.ReadAll(resp.Body)
	response := string(bodyBytes)

	// Should contain heartbeat comment
	if !strings.Contains(response, ": heartbeat") {
		t.Errorf("expected heartbeat in response, got:\n%s", response)
	}

	// Should also contain the event
	if !strings.Contains(response, `"id":1`) {
		t.Errorf("expected event in response, got:\n%s", response)
	}
}

func TestStream_MultipleStreamInterceptors(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {
			yield(StreamEvent{ID: 1, Message: "original"}, nil)
		}
	}

	// First interceptor: prepends "A-" to message
	interceptorA := func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error] {
		events := handler(ctx, req)
		return func(yield func(any, error) bool) {
			for event, err := range events {
				if err != nil {
					yield(nil, err)
					return
				}
				e := event.(StreamEvent)
				e.Message = "A-" + e.Message
				if !yield(e, nil) {
					return
				}
			}
		}
	}

	// Second interceptor: appends "-B" to message
	interceptorB := func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error] {
		events := handler(ctx, req)
		return func(yield func(any, error) bool) {
			for event, err := range events {
				if err != nil {
					yield(nil, err)
					return
				}
				e := event.(StreamEvent)
				e.Message = e.Message + "-B"
				if !yield(e, nil) {
					return
				}
			}
		}
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", streamIter2(fn).
		WithStreamInterceptor(interceptorA).
		WithStreamInterceptor(interceptorB))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Interceptors should chain: A runs first (outer), then B (inner)
	// So message goes: original -> A-original -> A-original-B
	events := parseSSEEvents(t, w.Body.String())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	expected := "A-original-B"
	if events[0].Result.Message != expected {
		t.Errorf("expected message %q, got %q", expected, events[0].Result.Message)
	}
}

func TestIsClientDisconnect(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"context.Canceled", context.Canceled, true},
		{"wrapped context.Canceled", fmt.Errorf("wrapped: %w", context.Canceled), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"client disconnected", errors.New("client disconnected"), true},
		{"random error", errors.New("database error"), false},
		{"timeout error", context.DeadlineExceeded, false}, // Not a disconnect
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isClientDisconnect(tt.err)
			if result != tt.expected {
				t.Errorf("isClientDisconnect(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestStreamEmit_SendWithID_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		shouldSend bool // whether the id should appear in response
	}{
		{"empty id", "", false},          // Empty ID should not produce id: field
		{"whitespace id", "   ", true},   // Whitespace is technically valid
		{"special chars", "a:b:c", true}, // Colons in ID
		{"unicode", "イベント-1", true},      // Unicode characters
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
				return e.SendWithID(tt.id, StreamEvent{ID: 1, Message: "test"})
			}

			app := NewApp()
			svc := app.Service("Feed")
			svc.Register("Subscribe", Stream(fn))

			body := `{"topic":"news"}`
			req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			app.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}

			response := w.Body.String()
			hasIDField := strings.Contains(response, "id: "+tt.id+"\n")

			if tt.shouldSend && !hasIDField {
				t.Errorf("expected id: %q in response, got:\n%s", tt.id, response)
			}
			if !tt.shouldSend && strings.Contains(response, "id:") {
				t.Errorf("expected no id field for empty id, got:\n%s", response)
			}
		})
	}
}

func TestStreamEmit_LastEventID_Missing(t *testing.T) {
	var receivedLastEventID string

	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		receivedLastEventID = e.LastEventID()
		return e.Send(StreamEvent{ID: 1})
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Intentionally NOT setting Last-Event-ID header
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Should be empty string when header is missing
	if receivedLastEventID != "" {
		t.Errorf("expected empty LastEventID when header missing, got %q", receivedLastEventID)
	}
}

func TestStreamEmit_ErrorAfterEvents(t *testing.T) {
	// Test that errors returned after sending events are properly sent to client
	fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
		if err := e.Send(StreamEvent{ID: 1, Message: "first"}); err != nil {
			return err
		}
		if err := e.Send(StreamEvent{ID: 2, Message: "second"}); err != nil {
			return err
		}
		// Return an error after successful events
		return NewError(CodeInternal, "something went wrong")
	}

	app := NewApp()
	svc := app.Service("Feed")
	svc.Register("Subscribe", Stream(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	response := w.Body.String()

	// Should have 2 success events
	events := parseSSEEvents(t, response)
	if len(events) != 2 {
		t.Errorf("expected 2 success events, got %d", len(events))
	}

	// Should also have error event at the end
	if !strings.Contains(response, `"error"`) {
		t.Errorf("expected error event in response, got:\n%s", response)
	}
	if !strings.Contains(response, "something went wrong") {
		t.Errorf("expected error message in response, got:\n%s", response)
	}
}
