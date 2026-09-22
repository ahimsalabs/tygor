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
	"sync"
	"sync/atomic"
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

type panicJSONEvent struct{}

func (panicJSONEvent) MarshalJSON() ([]byte, error) {
	panic("private marshaler panic")
}

type streamRecorder struct {
	*httptest.ResponseRecorder
}

type flushCapablePanicErrorResponseWriter struct {
	*panicErrorResponseWriter
}

func (*flushCapablePanicErrorResponseWriter) FlushError() error { return nil }

func newStreamRecorder() *streamRecorder {
	return &streamRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (w *streamRecorder) FlushError() error {
	w.ResponseRecorder.Flush()
	return nil
}

func (w *streamRecorder) SetWriteDeadline(time.Time) error { return nil }

func TestStream_Metadata(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		return func(yield func(StreamEvent, error) bool) {}
	}

	handler := streamIter2(fn)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}

	meta := handler.metadata()
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
	svc.register("Subscribe", streamIter2(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.register("Subscribe", streamIter2(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.register("Subscribe", streamIter2(fn))

	// Missing required "topic" field
	body := `{}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

	app.Handler().ServeHTTP(w, req)

	// Validation error should return before streaming starts
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStream_RejectsTrailingJSONBeforeStarting(t *testing.T) {
	called := false
	fn := func(ctx context.Context, req StreamRequest) iter.Seq2[StreamEvent, error] {
		called = true
		return func(yield func(StreamEvent, error) bool) {}
	}

	app := NewApp()
	app.Service("Feed").register("Subscribe", streamIter2(fn))
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader("{\"topic\":\"news\"}{\"topic\":\"other\"}"))
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
	if called {
		t.Fatal("stream handler ran before the complete request body was validated")
	}
	if got := w.Header().Get("Content-Type"); got == "text/event-stream" {
		t.Fatalf("streaming headers committed for invalid request: %q", got)
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

	app := NewApp(WithUnaryInterceptors(authInterceptor))
	svc := app.Service("Feed")
	svc.register("Subscribe", streamIter2(fn))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.register("Subscribe", streamIter2(fn, WithStreamInterceptors(transformInterceptor)))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.register("Subscribe", streamIter2(fn))

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
	svc.register("Subscribe", streamIter2(fn))

	// Try GET instead of POST
	req := httptest.NewRequest("GET", "/Feed/Subscribe?topic=news", nil)
	w := newStreamRecorder()

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
	svc.register("Subscribe", streamIter2(fn, WithoutValidation()))

	// Missing required field, but validation is skipped
	body := `{}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn)

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn)

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn)

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn)

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
	svc.Stream("Subscribe", fn)

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
	svc.Stream("Subscribe", fn,
		WithUnaryInterceptors(authInterceptor),
		WithStreamWriteTimeout(10*time.Second),
		WithoutValidation(),
	)

	// Missing required field, but validation is skipped
	body := `{}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn)

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Last-Event-ID", "42")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn)

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn, WithMaxRequestBodySize(10))

	// Send a body larger than the limit
	body := `{"topic":"this is a very long topic that exceeds the limit"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn, WithStreamHeartbeat(50*time.Millisecond))

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
	svc.register("Subscribe", streamIter2(fn, WithStreamInterceptors(interceptorA, interceptorB)))

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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

func TestStreamPreservesWrappedDeadlineErrorForPolicy(t *testing.T) {
	want := fmt.Errorf("query timed out: %w", context.DeadlineExceeded)
	var got error
	app := NewApp(WithStreamWriteTimeout(0), WithErrorTransformer(func(err error) *Error {
		got = err
		return DefaultErrorTransformer(err)
	}))
	app.Service("Feed").Stream("Subscribe", func(context.Context, StreamRequest, StreamWriter[StreamEvent]) error {
		return want
	})
	w := newStreamRecorder()
	app.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`)))
	if got != want {
		t.Fatalf("error policy received %v, want original wrapped error %v", got, want)
	}
	if !strings.Contains(w.Body.String(), `"code":"deadline_exceeded"`) {
		t.Fatalf("response = %q, want deadline_exceeded", w.Body.String())
	}
}

func TestStreamEmit_SendWithID_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty id", ""},           // Empty ID resets the last-event-ID value.
		{"whitespace id", "   "},   // Whitespace is technically valid.
		{"special chars", "a:b:c"}, // Colons in ID.
		{"unicode", "イベント-1"},      // Unicode characters.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := func(ctx context.Context, req StreamRequest, e StreamWriter[StreamEvent]) error {
				return e.SendWithID(tt.id, StreamEvent{ID: 1, Message: "test"})
			}

			app := NewApp()
			svc := app.Service("Feed")
			svc.Stream("Subscribe", fn)

			body := `{"topic":"news"}`
			req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := newStreamRecorder()

			app.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}

			response := w.Body.String()
			hasIDField := strings.Contains(response, "id: "+tt.id+"\n")
			if !hasIDField {
				t.Errorf("expected id: %q in response, got:\n%s", tt.id, response)
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
	svc.Stream("Subscribe", fn)

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Intentionally NOT setting Last-Event-ID header
	w := newStreamRecorder()

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
	svc.Stream("Subscribe", fn)

	body := `{"topic":"news"}`
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newStreamRecorder()

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

type blockingFlushWriter struct {
	header http.Header

	mu                sync.Mutex
	body              bytes.Buffer
	flushes           int
	eventFlushStarted chan struct{}
	releaseEventFlush chan struct{}
	flushErr          error
}

func newBlockingFlushWriter(flushErr error) *blockingFlushWriter {
	return &blockingFlushWriter{
		header:            make(http.Header),
		eventFlushStarted: make(chan struct{}),
		releaseEventFlush: make(chan struct{}),
		flushErr:          flushErr,
	}
}

func (w *blockingFlushWriter) Header() http.Header { return w.header }
func (w *blockingFlushWriter) WriteHeader(int)     {}

func (w *blockingFlushWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}

func (w *blockingFlushWriter) FlushError() error {
	w.mu.Lock()
	w.flushes++
	flushes := w.flushes
	w.mu.Unlock()
	if flushes == 2 {
		close(w.eventFlushStarted)
		<-w.releaseEventFlush
		return w.flushErr
	}
	return nil
}

func (w *blockingFlushWriter) SetWriteDeadline(time.Time) error { return nil }

type panicOnceWriter struct {
	header     http.Header
	panicValue any
	writes     int
}

func (w *panicOnceWriter) Header() http.Header { return w.header }
func (w *panicOnceWriter) WriteHeader(int)     {}

func (w *panicOnceWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		panic(w.panicValue)
	}
	return len(p), nil
}

func (w *panicOnceWriter) FlushError() error                { return nil }
func (w *panicOnceWriter) SetWriteDeadline(time.Time) error { return nil }

type retryPoisonWriter struct {
	header     http.Header
	body       bytes.Buffer
	writeErr   error
	panicValue any
	writes     int
	flushes    int
}

func (w *retryPoisonWriter) Header() http.Header { return w.header }
func (w *retryPoisonWriter) WriteHeader(int)     {}

func (w *retryPoisonWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 && len(p) > 0 {
		n, _ := w.body.Write(p[:1])
		if w.panicValue != nil {
			panic(w.panicValue)
		}
		return n, w.writeErr
	}
	return w.body.Write(p)
}

func (w *retryPoisonWriter) FlushError() error {
	w.flushes++
	return nil
}

func (w *retryPoisonWriter) SetWriteDeadline(time.Time) error { return nil }

type failingCommittedWriter struct {
	header  http.Header
	writes  int
	partial bool
}

func (w *failingCommittedWriter) Header() http.Header { return w.header }
func (w *failingCommittedWriter) WriteHeader(int)     {}

func (w *failingCommittedWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.partial && len(p) > 0 {
		return 1, errors.New("partial transport write")
	}
	return 0, errors.New("terminal transport write failed")
}

func (w *failingCommittedWriter) FlushError() error                { return nil }
func (w *failingCommittedWriter) SetWriteDeadline(time.Time) error { return nil }

func TestStreamSendWaitsForFlushAndReturnsFailure(t *testing.T) {
	w := newBlockingFlushWriter(timeoutError{})
	sendResult := make(chan error, 1)
	done := make(chan struct{})
	handlerPanic := make(chan any, 1)

	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		err := stream.Send(StreamEvent{ID: 1, Message: "event"})
		sendResult <- err
		return err
	}
	app := NewApp()
	app.Service("Feed").Stream("Subscribe", fn)
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))

	go func() {
		defer func() {
			handlerPanic <- recover()
			close(done)
		}()
		app.Handler().ServeHTTP(w, req)
	}()

	select {
	case <-w.eventFlushStarted:
	case <-time.After(time.Second):
		t.Fatal("event flush did not start")
	}
	select {
	case err := <-sendResult:
		t.Fatalf("Send returned before flush completed: %v", err)
	default:
	}

	close(w.releaseEventFlush)
	select {
	case err := <-sendResult:
		if !errors.Is(err, ErrStreamClosed) {
			t.Fatalf("Send error = %v, want ErrStreamClosed", err)
		}
		if !errors.Is(err, ErrWriteTimeout) {
			t.Fatalf("Send error = %v, want ErrWriteTimeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not return after flush failed")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not terminate after flush failed")
	}
	if recovered := <-handlerPanic; recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
}

func TestStreamFailureWaitsForProducerAfterUnarySetup(t *testing.T) {
	w := newBlockingFlushWriter(timeoutError{})
	producerCleanupStarted := make(chan struct{})
	releaseProducerCleanup := make(chan struct{})
	producerDone := make(chan struct{})
	interceptorExited := make(chan struct{})
	handlerDone := make(chan struct{})
	handlerPanic := make(chan any, 1)

	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		defer close(producerDone)
		err := stream.Send(StreamEvent{ID: 1, Message: "event"})
		close(producerCleanupStarted)
		<-releaseProducerCleanup
		return err
	}
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		result, err := handler(ctx, req)
		close(interceptorExited)
		return result, err
	}
	app := NewApp(WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", fn)
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))

	go func() {
		defer func() {
			handlerPanic <- recover()
			close(handlerDone)
		}()
		app.Handler().ServeHTTP(w, req)
	}()

	select {
	case <-w.eventFlushStarted:
	case <-time.After(time.Second):
		t.Fatal("event flush did not start")
	}
	select {
	case <-interceptorExited:
	default:
		t.Fatal("unary setup interceptor did not exit before streaming")
	}
	close(w.releaseEventFlush)
	select {
	case <-producerCleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("producer did not begin cleanup")
	}
	close(releaseProducerCleanup)
	for name, ch := range map[string]<-chan struct{}{
		"producer": producerDone,
		"handler":  handlerDone,
	} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("%s did not finish", name)
		}
	}
	if recovered := <-handlerPanic; recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
}

func TestStreamHeartbeatFailureCancelsProducerAfterUnarySetup(t *testing.T) {
	w := newBlockingFlushWriter(timeoutError{})
	producerCanceled := make(chan struct{})
	releaseProducerCleanup := make(chan struct{})
	producerDone := make(chan struct{})
	interceptorExited := make(chan struct{})
	handlerDone := make(chan struct{})
	handlerPanic := make(chan any, 1)

	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		defer close(producerDone)
		<-ctx.Done()
		close(producerCanceled)
		<-releaseProducerCleanup
		return ctx.Err()
	}
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		result, err := handler(ctx, req)
		close(interceptorExited)
		return result, err
	}
	app := NewApp(WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", fn, WithStreamHeartbeat(time.Millisecond))
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))

	go func() {
		defer func() {
			handlerPanic <- recover()
			close(handlerDone)
		}()
		app.Handler().ServeHTTP(w, req)
	}()

	select {
	case <-w.eventFlushStarted:
	case <-time.After(time.Second):
		t.Fatal("heartbeat flush did not start")
	}
	select {
	case <-interceptorExited:
	default:
		t.Fatal("unary setup interceptor did not exit before streaming")
	}
	close(w.releaseEventFlush)
	select {
	case <-producerCanceled:
	case <-time.After(time.Second):
		t.Fatal("heartbeat failure did not cancel producer context")
	}
	close(releaseProducerCleanup)
	for name, ch := range map[string]<-chan struct{}{
		"producer": producerDone,
		"handler":  handlerDone,
	} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("%s did not finish", name)
		}
	}
	if recovered := <-handlerPanic; recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
}

func TestStreamTransportPanicIsNotConvertedToStreamError(t *testing.T) {
	panicValue := &struct{}{}
	w := &panicOnceWriter{header: make(http.Header), panicValue: panicValue}
	producerDone := make(chan struct{})
	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		defer close(producerDone)
		return stream.Send(StreamEvent{ID: 1, Message: "event"})
	}
	app := NewApp(WithStreamWriteTimeout(time.Second))
	app.Service("Feed").Stream("Subscribe", fn)
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))

	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
		return nil
	}()
	if recovered != panicValue {
		t.Fatalf("recovered panic = %v, want original transport panic", recovered)
	}
	if w.writes != 1 {
		t.Fatalf("writes = %d, want no terminal error write after panic", w.writes)
	}
	select {
	case <-producerDone:
	case <-time.After(time.Second):
		t.Fatal("producer did not stop after transport panic")
	}
}

func TestStreamUnaryErrorTransportPanicDoesNotRetryPolicy(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		flush       bool
		interceptor bool
	}{
		{name: "decode failure", body: `{`},
		{name: "capability preflight failure", body: `{}`},
		{name: "interceptor rejection", body: `{}`, flush: true, interceptor: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var transformerCalls atomic.Int32
			var producerStarts atomic.Int32
			options := []AppOption{WithStreamWriteTimeout(0), WithErrorTransformer(func(error) *Error {
				transformerCalls.Add(1)
				panic("private transformer panic")
			})}
			if tt.interceptor {
				options = append(options, WithUnaryInterceptors(func(Context, any, HandlerFunc) (any, error) {
					return nil, NewError(CodeUnavailable, "private interceptor rejection")
				}))
			}
			app := NewApp(options...)
			app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, _ StreamWriter[int]) error {
				producerStarts.Add(1)
				return nil
			})

			baseWriter := &panicErrorResponseWriter{header: make(http.Header)}
			var writer http.ResponseWriter = baseWriter
			if tt.flush {
				writer = &flushCapablePanicErrorResponseWriter{panicErrorResponseWriter: baseWriter}
			}
			req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				app.Handler().ServeHTTP(writer, req)
			}()

			if recovered != "transport panic" {
				t.Fatalf("recovered = %v, want original transport panic", recovered)
			}
			if got := transformerCalls.Load(); got != 1 {
				t.Fatalf("transformer calls = %d, want exactly 1", got)
			}
			if baseWriter.statusCalls != 1 || baseWriter.writes != 1 {
				t.Fatalf("transport attempts = status:%d write:%d, want 1/1", baseWriter.statusCalls, baseWriter.writes)
			}
			if producerStarts.Load() != 0 {
				t.Fatal("stream producer started before unary error response")
			}
		})
	}
}

func TestStreamProducerPanicFailedTerminalWriteAbortsTransport(t *testing.T) {
	app := NewApp(WithStreamWriteTimeout(0))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, _ StreamWriter[StreamEvent]) error {
		panic("private producer panic")
	})
	w := &failingCommittedWriter{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
	if w.writes != 1 {
		t.Fatalf("writes = %d, want one failed terminal frame", w.writes)
	}
}

func TestStreamUnarySetupPanicDoesNotCommit(t *testing.T) {
	panicValue := &struct{}{}
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		_, _ = handler(ctx, req)
		panic(panicValue)
	}
	app := NewApp(WithStreamWriteTimeout(0), WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[StreamEvent]) error {
		return stream.Send(StreamEvent{ID: 1})
	})
	w := newStreamRecorder()
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if recovered != nil {
		t.Fatalf("recovered = %v, want unary error handling", recovered)
	}
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), `"code":"internal"`) {
		t.Fatalf("response = %d %q, want unary internal error", w.Code, w.Body.String())
	}
}

func TestStreamEventWriteFailureAbortsTransport(t *testing.T) {
	app := NewApp(WithStreamWriteTimeout(0))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[StreamEvent]) error {
		return stream.Send(StreamEvent{ID: 1})
	})
	w := &failingCommittedWriter{header: make(http.Header), partial: true}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
	if w.writes != 1 {
		t.Fatalf("writes = %d, want one partial event write", w.writes)
	}
}

func TestStreamHeartbeatWriteFailureAbortsTransport(t *testing.T) {
	app := NewApp(WithStreamWriteTimeout(0))
	app.Service("Feed").Stream("Subscribe", func(ctx context.Context, _ Empty, _ StreamWriter[StreamEvent]) error {
		<-ctx.Done()
		return ctx.Err()
	}, WithStreamHeartbeat(time.Millisecond))
	w := &failingCommittedWriter{header: make(http.Header), partial: true}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
	if w.writes != 1 {
		t.Fatalf("writes = %d, want one partial heartbeat write", w.writes)
	}
}

func TestStreamInterceptorCannotSuppressTransportFailure(t *testing.T) {
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		_, _ = handler(ctx, req)
		return nil, nil
	}
	app := NewApp(WithStreamWriteTimeout(0), WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[StreamEvent]) error {
		return stream.Send(StreamEvent{ID: 1})
	})
	w := &failingCommittedWriter{header: make(http.Header), partial: true}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
}

func TestStreamInterceptorCannotRecoverTransportPanic(t *testing.T) {
	interceptor := func(ctx Context, req any, handler HandlerFunc) (result any, err error) {
		defer func() { _ = recover() }()
		return handler(ctx, req)
	}
	app := NewApp(WithStreamWriteTimeout(0), WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[StreamEvent]) error {
		return stream.Send(StreamEvent{ID: 1})
	})
	panicValue := &struct{}{}
	w := &panicOnceWriter{header: make(http.Header), panicValue: panicValue}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if recovered != panicValue {
		t.Fatalf("recovered = %v, want original transport panic", recovered)
	}
}

func TestStreamUnaryRetryDoesNotReplayTransportFailure(t *testing.T) {
	setupCalls := 0
	producerCalls := 0
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		setupCalls++
		_, _ = handler(ctx, req)
		setupCalls++
		_, _ = handler(ctx, req)
		return nil, nil
	}
	app := NewApp(WithStreamWriteTimeout(0), WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[StreamEvent]) error {
		producerCalls++
		return stream.Send(StreamEvent{ID: 1})
	})
	w := &retryPoisonWriter{header: make(http.Header), writeErr: errors.New("partial transport write")}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if setupCalls != 2 || producerCalls != 1 {
		t.Fatalf("setup calls = %d, producer calls = %d; want 2, 1", setupCalls, producerCalls)
	}
	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
	}
	if w.writes != 1 || w.flushes != 1 || w.body.String() != "d" {
		t.Fatalf("transport operations after retry: writes=%d flushes=%d body=%q, want 1, 1, prefix only", w.writes, w.flushes, w.body.String())
	}
}

func TestStreamUnaryRetryDoesNotReplayTransportPanic(t *testing.T) {
	panicValue := &struct{}{}
	setupCalls := 0
	producerCalls := 0
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		setupCalls++
		_, _ = handler(ctx, req)
		setupCalls++
		_, _ = handler(ctx, req)
		return nil, nil
	}
	app := NewApp(WithStreamWriteTimeout(0), WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[StreamEvent]) error {
		producerCalls++
		return stream.Send(StreamEvent{ID: 1})
	})
	w := &retryPoisonWriter{header: make(http.Header), panicValue: panicValue}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if setupCalls != 2 || producerCalls != 1 {
		t.Fatalf("setup calls = %d, producer calls = %d; want 2, 1", setupCalls, producerCalls)
	}
	if recovered != panicValue {
		t.Fatalf("outer recovered = %v, want original transport panic", recovered)
	}
	if w.writes != 1 || w.flushes != 1 || w.body.String() != "d" {
		t.Fatalf("transport operations after retry: writes=%d flushes=%d body=%q, want 1, 1, prefix only", w.writes, w.flushes, w.body.String())
	}
}

func TestStreamInterceptorCannotSuppressMarshalPanic(t *testing.T) {
	interceptor := func(ctx Context, req any, handler HandlerFunc) (result any, err error) {
		defer func() { _ = recover() }()
		return handler(ctx, req)
	}
	app := NewApp(WithMaskInternalErrors(), WithStreamWriteTimeout(0), WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[panicJSONEvent]) error {
		return stream.Send(panicJSONEvent{})
	})
	w := newStreamRecorder()
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	app.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Count(body, "data: ") != 1 || !strings.Contains(body, `"code":"internal"`) {
		t.Fatalf("suppressed marshaler panic did not produce one terminal SSE frame: %q", body)
	}
}

func TestStreamInterceptorCannotSuppressProducerPanic(t *testing.T) {
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		_, _ = handler(ctx, req)
		return nil, nil
	}
	app := NewApp(WithMaskInternalErrors(), WithStreamWriteTimeout(0), WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ Empty, stream StreamWriter[StreamEvent]) error {
		if err := stream.Send(StreamEvent{ID: 1, Message: "before panic"}); err != nil {
			return err
		}
		panic("private producer panic")
	})
	w := newStreamRecorder()
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", nil)

	app.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Count(body, "data: ") != 2 || !strings.Contains(body, "before panic") || !strings.Contains(body, `"code":"internal"`) {
		t.Fatalf("suppressed producer panic did not produce an event and terminal SSE frame: %q", body)
	}
	if strings.Contains(body, "private producer panic") {
		t.Fatalf("producer panic leaked to SSE response: %q", body)
	}
}

func TestStreamSendAllowsInterceptorToFilterEvent(t *testing.T) {
	sendResult := make(chan error, 1)
	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		err := stream.Send(StreamEvent{ID: 1, Message: "filtered"})
		sendResult <- err
		return err
	}
	filter := func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error] {
		events := handler(ctx, req)
		return func(yield func(any, error) bool) {
			for range events {
				// A filtered event was accepted by the pipeline but has no frame.
			}
		}
	}

	app := NewApp()
	app.Service("Feed").Stream("Subscribe", fn, WithStreamInterceptors(filter))
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))
	w := newStreamRecorder()
	app.Handler().ServeHTTP(w, req)

	if err := <-sendResult; err != nil {
		t.Fatalf("Send error = %v, want nil for filtered event", err)
	}
	if strings.Contains(w.Body.String(), "data:") {
		t.Fatalf("filtered event wrote a frame: %s", w.Body.String())
	}
}

func TestStreamUnaryInterceptorsAreSetupOnly(t *testing.T) {
	type ctxKey string
	key := ctxKey("principal")
	deadline := time.Now().Add(time.Minute)
	callbackErr := make(chan error, 1)

	outer := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		modified := req.(StreamRequest)
		modified.Topic = "modified"
		derived := context.WithValue(ctx, key, "alice")
		derived, cancel := context.WithDeadline(derived, deadline)
		defer cancel()
		return handler(derived, modified)
	}
	inner := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		if ctx.Value(key) != "alice" {
			return nil, errors.New("inner interceptor lost context value")
		}
		if got, ok := ctx.Deadline(); !ok || !got.Equal(deadline) {
			return nil, errors.New("inner interceptor lost deadline")
		}
		return handler(ctx, req)
	}
	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		if req.Topic != "original" {
			callbackErr <- fmt.Errorf("request topic = %q, want original", req.Topic)
			return nil
		}
		if ctx.Value(key) != nil || ctx.Err() != nil {
			callbackErr <- fmt.Errorf("callback inherited setup context value/error = %v/%v", ctx.Value(key), ctx.Err())
			return nil
		}
		if got, ok := ctx.Deadline(); ok {
			callbackErr <- fmt.Errorf("callback inherited setup deadline %v", got)
			return nil
		}
		callbackErr <- nil
		return stream.Send(StreamEvent{ID: 1})
	}

	app := NewApp(WithUnaryInterceptors(outer), WithUnaryInterceptors(inner))
	app.Service("Feed").Stream("Subscribe", fn)
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"original"}`))
	w := newStreamRecorder()
	app.Handler().ServeHTTP(w, req)

	if err := <-callbackErr; err != nil {
		t.Fatal(err)
	}
	if events := parseSSEEvents(t, w.Body.String()); len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
}

func TestStreamUnaryInterceptorDeadlineDoesNotGovernStreamLifetime(t *testing.T) {
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		derived, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		return handler(derived, req)
	}
	producerCanceled := make(chan error, 1)
	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		<-ctx.Done()
		producerCanceled <- ctx.Err()
		return ctx.Err()
	}

	app := NewApp(WithUnaryInterceptors(interceptor))
	app.Service("Feed").Stream("Subscribe", fn)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`)).WithContext(requestCtx)
	w := newStreamRecorder()
	done := make(chan struct{})
	go func() {
		app.Handler().ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-producerCanceled:
		t.Fatalf("setup deadline terminated stream: %v", err)
	default:
	}
	cancelRequest()
	select {
	case err := <-producerCanceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("producer cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not stop producer")
	}
	<-done
}

func TestStreamInterceptorContextAndRequestReachSource(t *testing.T) {
	type ctxKey string
	key := ctxKey("stream-value")
	callbackErr := make(chan error, 1)

	interceptor := func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error] {
		modified := req.(StreamRequest)
		modified.Topic = "modified"
		return handler(context.WithValue(ctx, key, "present"), modified)
	}
	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		if req.Topic != "modified" || ctx.Value(key) != "present" {
			callbackErr <- fmt.Errorf("source got topic/value %q/%v", req.Topic, ctx.Value(key))
			return nil
		}
		callbackErr <- nil
		return stream.Send(StreamEvent{ID: 1})
	}

	app := NewApp()
	app.Service("Feed").Stream("Subscribe", fn, WithStreamInterceptors(interceptor))
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"original"}`))
	w := newStreamRecorder()
	app.Handler().ServeHTTP(w, req)

	if err := <-callbackErr; err != nil {
		t.Fatal(err)
	}
}

func TestStreamErrorUsesConfiguredPolicyWithoutMutatingSource(t *testing.T) {
	t.Run("custom transformer", func(t *testing.T) {
		applicationErr := errors.New("private application failure")
		fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
			return applicationErr
		}
		app := NewApp(WithErrorTransformer(func(err error) *Error {
			if errors.Is(err, applicationErr) {
				return NewError(CodeUnavailable, "safe transformed failure")
			}
			return nil
		}))
		app.Service("Feed").Stream("Subscribe", fn)
		req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))
		w := newStreamRecorder()
		app.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		if !strings.Contains(body, `"code":"unavailable"`) || !strings.Contains(body, "safe transformed failure") {
			t.Fatalf("SSE error did not use custom transformer: %s", body)
		}
		if strings.Contains(body, applicationErr.Error()) {
			t.Fatalf("SSE error leaked source error: %s", body)
		}
	})

	t.Run("mask owns transformed error", func(t *testing.T) {
		shared := NewError(CodeInternal, "private shared failure").WithDetail("source", "database")
		fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
			return shared
		}
		app := NewApp(WithMaskInternalErrors())
		app.Service("Feed").Stream("Subscribe", fn)
		req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))
		w := newStreamRecorder()
		app.Handler().ServeHTTP(w, req)

		if shared.Message != "private shared failure" {
			t.Fatalf("shared error message was mutated to %q", shared.Message)
		}
		body := w.Body.String()
		if strings.Contains(body, shared.Message) || !strings.Contains(body, "internal server error") {
			t.Fatalf("SSE error was not masked: %s", body)
		}
	})
}

func TestStreamErrorPolicyPanicFallsBackOverSSE(t *testing.T) {
	tests := []struct {
		name        string
		transformer func(*atomic.Int32) ErrorTransformer
	}{
		{
			name: "panicking transformer",
			transformer: func(calls *atomic.Int32) ErrorTransformer {
				return func(error) *Error {
					calls.Add(1)
					panic("private transformer panic")
				}
			},
		},
		{
			name: "error envelope serialization panic",
			transformer: func(calls *atomic.Int32) ErrorTransformer {
				return func(error) *Error {
					calls.Add(1)
					return NewError(CodeUnavailable, "private transformed error").WithDetail("value", panicJSONDetail{})
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			app := NewApp(WithErrorTransformer(tt.transformer(&calls)))
			app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ StreamRequest, stream StreamWriter[StreamEvent]) error {
				if err := stream.Send(StreamEvent{ID: 1, Message: "before error"}); err != nil {
					return err
				}
				return errors.New("private terminal error")
			})
			server := httptest.NewServer(app.Handler())
			t.Cleanup(server.Close)

			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Post(server.URL+"/Feed/Subscribe", "application/json", strings.NewReader(`{"topic":"news"}`))
			if err != nil {
				t.Fatalf("POST error = %v, want framed SSE fallback", err)
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatalf("response = %d %q, want committed SSE", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			if strings.Count(string(body), "data: ") != 2 || !strings.Contains(string(body), "before error") || !strings.HasSuffix(string(body), "data: {\"error\":{\"code\":\"internal\",\"message\":\"internal server error\"}}\n\n") {
				t.Fatalf("SSE body = %q, want event plus constant framed fallback", body)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("transformer calls = %d, want exactly 1", got)
			}
		})
	}
}

func TestStreamCallbackPanicUsesConfiguredErrorPolicy(t *testing.T) {
	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		panic("private stream panic")
	}
	app := NewApp(WithErrorTransformer(func(err error) *Error {
		var svcErr *Error
		if errors.As(err, &svcErr) && svcErr.Code == CodeInternal {
			return NewError(CodeUnavailable, "safe panic response")
		}
		return nil
	}))
	app.Service("Feed").Stream("Subscribe", fn)
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))
	w := newStreamRecorder()
	app.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "private stream panic") {
		t.Fatalf("panic value leaked to SSE response: %s", body)
	}
	if !strings.Contains(body, `"code":"unavailable"`) || !strings.Contains(body, "safe panic response") {
		t.Fatalf("panic did not use configured error policy: %s", body)
	}
}

func TestStreamMarshalPanicAfterCommitUsesSSEErrorPolicy(t *testing.T) {
	app := NewApp(WithMaskInternalErrors(), WithStreamWriteTimeout(0), WithErrorTransformer(func(err error) *Error {
		var svcErr *Error
		if errors.As(err, &svcErr) && svcErr.Code == CodeInternal {
			return NewError(CodeUnavailable, "safe marshaler panic")
		}
		return nil
	}))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ StreamRequest, stream StreamWriter[panicJSONEvent]) error {
		return stream.Send(panicJSONEvent{})
	})
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))
	w := newStreamRecorder()

	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("response = %d %q, want committed SSE", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	if strings.Count(body, "data: ") != 1 || !strings.HasPrefix(body, "data: ") || !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("panic response is not one complete SSE frame: %q", body)
	}
	if strings.Contains(body, "private marshaler panic") || !strings.Contains(body, `"code":"unavailable"`) || !strings.Contains(body, "safe marshaler panic") {
		t.Fatalf("panic response did not use configured policy: %s", body)
	}
}

func TestStreamSendWithEmptyIDEmitsResetField(t *testing.T) {
	app := NewApp(WithStreamWriteTimeout(0))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ StreamRequest, stream StreamWriter[StreamEvent]) error {
		return stream.SendWithID("", StreamEvent{ID: 1})
	})
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))
	w := newStreamRecorder()

	app.Handler().ServeHTTP(w, req)

	if body := w.Body.String(); !strings.HasPrefix(body, "id: \ndata: ") {
		t.Fatalf("empty event ID did not emit reset field: %q", body)
	}
}

func TestStreamSendWithIDRejectsUnsafeFraming(t *testing.T) {
	for _, id := range []string{"line\nnext", "line\rnext", "line\r\nnext", "nul\x00next"} {
		t.Run(fmt.Sprintf("%q", id), func(t *testing.T) {
			yielded := false
			sender := &streamSender[StreamEvent]{
				ctx: context.Background(),
				yieldAny: func(any, error) bool {
					yielded = true
					return true
				},
			}
			err := sender.SendWithID(id, StreamEvent{ID: 1})
			if !errors.Is(err, ErrInvalidStreamEventID) {
				t.Fatalf("SendWithID() error = %v, want ErrInvalidStreamEventID", err)
			}
			if yielded {
				t.Fatal("unsafe ID reached the stream pipeline")
			}
			if _, err := marshalSSEEventFrame(sseEvent{id: id, event: StreamEvent{ID: 1}}); !errors.Is(err, ErrInvalidStreamEventID) {
				t.Fatalf("marshalSSEEventFrame() error = %v, want ErrInvalidStreamEventID", err)
			}
		})
	}
}

func TestStreamAcceptsRecorderWithoutDeadlineSupport(t *testing.T) {
	producerStarted := false
	fn := func(ctx context.Context, req StreamRequest, stream StreamWriter[StreamEvent]) error {
		producerStarted = true
		return nil
	}
	app := NewApp()
	app.Service("Feed").Stream("Subscribe", fn)
	req := httptest.NewRequest("POST", "/Feed/Subscribe", strings.NewReader(`{"topic":"news"}`))
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("response = %d %q, want successful SSE", w.Code, w.Header().Get("Content-Type"))
	}
	if !producerStarted {
		t.Fatal("producer did not start with a flush-capable recorder")
	}
}
