package tygor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAtom_GetSet(t *testing.T) {
	atom := NewAtom(42)

	if got := atom.Get(); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	atom.Set(100)
	if got := atom.Get(); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}
}

func TestAtom_Update(t *testing.T) {
	atom := NewAtom(10)

	atom.Update(func(v int) int {
		return v * 2
	})

	if got := atom.Get(); got != 20 {
		t.Errorf("expected 20, got %d", got)
	}
}

func TestAtom_Subscribe(t *testing.T) {
	atom := NewAtom("initial")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var values []string
	done := make(chan struct{})

	go func() {
		for v := range atom.Subscribe(ctx) {
			values = append(values, v)
			if len(values) >= 3 {
				cancel()
			}
		}
		close(done)
	}()

	// Give subscriber time to start
	time.Sleep(10 * time.Millisecond)

	atom.Set("second")
	atom.Set("third")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't complete")
	}

	if len(values) < 3 {
		t.Errorf("expected at least 3 values, got %d: %v", len(values), values)
	}
	if values[0] != "initial" {
		t.Errorf("expected first value 'initial', got %q", values[0])
	}
}

func TestAtom_Close(t *testing.T) {
	atom := NewAtom("value")

	// Subscribe before close
	ctx := context.Background()
	subscribeDone := make(chan struct{})

	go func() {
		for range atom.Subscribe(ctx) {
		}
		close(subscribeDone)
	}()

	// Give subscriber time to start
	time.Sleep(10 * time.Millisecond)

	// Close the atom
	atom.Close()

	// Subscriber should exit
	select {
	case <-subscribeDone:
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't exit after Close")
	}

	// Set should be no-op after close
	atom.Set("new value")
	if got := atom.Get(); got != "value" {
		t.Errorf("Set should be no-op after Close, got %q", got)
	}

	// New subscriptions should return immediately
	subscribeAfterClose := make(chan struct{})
	go func() {
		for range atom.Subscribe(ctx) {
			t.Error("should not yield any values after Close")
		}
		close(subscribeAfterClose)
	}()

	select {
	case <-subscribeAfterClose:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Subscribe should return immediately after Close")
	}
}

func TestAtom_CloseIdempotent(t *testing.T) {
	atom := NewAtom(1)

	// Close multiple times should not panic
	atom.Close()
	atom.Close()
	atom.Close()
}

func TestAtom_ConcurrentAccess(t *testing.T) {
	atom := NewAtom(0)

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOps = 100

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				atom.Set(j)
			}
		}()
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				atom.Get()
			}
		}()
	}

	// Concurrent updaters
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				atom.Update(func(v int) int { return v + 1 })
			}
		}()
	}

	wg.Wait()
}

func TestAtomHandler_Metadata(t *testing.T) {
	type Status struct {
		State string `json:"state"`
	}

	atom := NewAtom(&Status{State: "idle"})
	handler := atom.Handler()

	meta := handler.Metadata()
	if meta.Primitive != "atom" {
		t.Errorf("expected primitive 'atom', got %q", meta.Primitive)
	}
}

func TestAtomHandler_SSE(t *testing.T) {
	type Status struct {
		State string `json:"state"`
	}

	atom := NewAtom(&Status{State: "idle"})

	app := NewApp()
	svc := app.Service("System")
	svc.Register("Status", atom.Handler())

	// Start request (atom uses POST)
	req := httptest.NewRequest("POST", "/System/Status", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Run handler in goroutine since it blocks
	done := make(chan struct{})
	go func() {
		app.Handler().ServeHTTP(w, req)
		close(done)
	}()

	// Give handler time to send initial value
	time.Sleep(50 * time.Millisecond)

	// Close atom to terminate the handler
	atom.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler didn't exit")
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", contentType)
	}

	// Should have initial value
	body := w.Body.String()
	if !strings.Contains(body, `"state":"idle"`) {
		t.Errorf("expected initial state in response, got:\n%s", body)
	}
}

func TestAtomHandler_SSE_Updates(t *testing.T) {
	type Counter struct {
		Value int `json:"value"`
	}

	atom := NewAtom(&Counter{Value: 0})

	app := NewApp()
	svc := app.Service("System")
	svc.Register("Counter", atom.Handler())

	server := httptest.NewServer(app.Handler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/System/Counter", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	// Start streaming request
	respChan := make(chan *http.Response, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			respChan <- resp
		}
	}()

	// Wait for connection to be established
	time.Sleep(50 * time.Millisecond)

	// Send updates
	atom.Set(&Counter{Value: 1})
	atom.Set(&Counter{Value: 2})

	// Give time for updates to be sent
	time.Sleep(50 * time.Millisecond)

	// Cancel to stop the stream
	cancel()

	// Cleanup
	select {
	case resp := <-respChan:
		resp.Body.Close()
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAtomHandler_ClosedAtom(t *testing.T) {
	atom := NewAtom("value")
	atom.Close()

	app := NewApp()
	svc := app.Service("System")
	svc.Register("Status", atom.Handler())

	req := httptest.NewRequest("POST", "/System/Status", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should return error for closed atom
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d: %s", w.Code, w.Body.String())
	}

	// Verify error envelope format
	body := w.Body.String()
	if !strings.Contains(body, `"error"`) {
		t.Errorf("expected error envelope, got: %s", body)
	}
	if !strings.Contains(body, `"code":"unavailable"`) {
		t.Errorf("expected code 'unavailable', got: %s", body)
	}
	if !strings.Contains(body, `"atom closed"`) {
		t.Errorf("expected message 'atom closed', got: %s", body)
	}
}

func TestAtomHandler_WithOptions(t *testing.T) {
	atom := NewAtom("value")

	authInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return nil, NewError(CodeUnauthenticated, "not authorized")
	}

	app := NewApp()
	svc := app.Service("System")
	svc.Register("Status", atom.Handler().
		WithUnaryInterceptor(authInterceptor).
		WithWriteTimeout(10*time.Second).
		WithHeartbeat(30*time.Second))

	req := httptest.NewRequest("POST", "/System/Status", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should be rejected by interceptor
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAtom_SubscriberGetsLatestValue(t *testing.T) {
	// Test that slow subscribers get latest value (not queued intermediate values)
	atom := NewAtom(0)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var received []int
	started := make(chan struct{})
	var once sync.Once

	// Start a goroutine that sends updates continuously
	go func() {
		<-started
		for i := 1; i <= 1000; i++ {
			atom.Set(i)
			time.Sleep(time.Millisecond) // Spread out updates
		}
	}()

	for v := range atom.Subscribe(ctx) {
		received = append(received, v)
		once.Do(func() { close(started) })
		// Slow subscriber - context timeout will terminate
		time.Sleep(50 * time.Millisecond)
	}

	// Should have received initial value (0) and at least one update
	// Due to latest-wins semantics, might skip intermediate values
	if len(received) < 2 {
		t.Errorf("expected at least 2 values, got %d: %v", len(received), received)
	}
	if received[0] != 0 {
		t.Errorf("first value should be initial (0), got %d", received[0])
	}
}
