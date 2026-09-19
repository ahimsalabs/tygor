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

func TestLiveValue_GetSet(t *testing.T) {
	lv := NewLiveValue(42)

	if got := lv.Get(); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	lv.Set(100)
	if got := lv.Get(); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}
}

func TestLiveValue_Update(t *testing.T) {
	lv := NewLiveValue(10)

	lv.Update(func(v int) int {
		return v * 2
	})

	if got := lv.Get(); got != 20 {
		t.Errorf("expected 20, got %d", got)
	}
}

func TestLiveValue_Subscribe(t *testing.T) {
	lv := NewLiveValue("initial")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var values []string
	done := make(chan struct{})

	go func() {
		for v := range lv.Subscribe(ctx) {
			values = append(values, v)
			if len(values) >= 3 {
				cancel()
			}
		}
		close(done)
	}()

	// Give subscriber time to start
	time.Sleep(10 * time.Millisecond)

	lv.Set("second")
	lv.Set("third")

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

func TestLiveValue_Close(t *testing.T) {
	lv := NewLiveValue("value")

	// Subscribe before close
	ctx := context.Background()
	subscribeDone := make(chan struct{})

	go func() {
		for range lv.Subscribe(ctx) {
		}
		close(subscribeDone)
	}()

	// Give subscriber time to start
	time.Sleep(10 * time.Millisecond)

	// Close the livevalue
	lv.Close()

	// Subscriber should exit
	select {
	case <-subscribeDone:
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't exit after Close")
	}

	// Set should be no-op after close
	lv.Set("new value")
	if got := lv.Get(); got != "value" {
		t.Errorf("Set should be no-op after Close, got %q", got)
	}

	// New subscriptions should return immediately
	subscribeAfterClose := make(chan struct{})
	go func() {
		for range lv.Subscribe(ctx) {
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

func TestLiveValue_CloseIdempotent(t *testing.T) {
	lv := NewLiveValue(1)

	// Close multiple times should not panic
	lv.Close()
	lv.Close()
	lv.Close()
}

func TestLiveValue_ConcurrentAccess(t *testing.T) {
	lv := NewLiveValue(0)

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOps = 100

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				lv.Set(j)
			}
		}()
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				lv.Get()
			}
		}()
	}

	// Concurrent updaters
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				lv.Update(func(v int) int { return v + 1 })
			}
		}()
	}

	wg.Wait()
}

func TestLiveValueHandler_Metadata(t *testing.T) {
	type Status struct {
		State string `json:"state"`
	}

	lv := NewLiveValue(&Status{State: "idle"})
	app := NewApp()
	app.Service("System").LiveValue("Status", lv)

	app.mu.RLock()
	handler, ok := app.routes["System.Status"].(*liveValueHandler[*Status])
	app.mu.RUnlock()
	if !ok {
		t.Fatal("live value route did not contain the typed live value handler")
	}

	meta := handler.metadata()
	if meta.Primitive != "livevalue" {
		t.Errorf("expected primitive 'livevalue', got %q", meta.Primitive)
	}
}

func TestLiveValueHandler_SSE(t *testing.T) {
	type Status struct {
		State string `json:"state"`
	}

	lv := NewLiveValue(&Status{State: "idle"})

	app := NewApp()
	svc := app.Service("System")
	svc.LiveValue("Status", lv)

	// Start request (livevalue uses POST)
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

	// Close livevalue to terminate the handler
	lv.Close()

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

func TestLiveValueHandler_SSE_Updates(t *testing.T) {
	type Counter struct {
		Value int `json:"value"`
	}

	lv := NewLiveValue(&Counter{Value: 0})

	app := NewApp()
	svc := app.Service("System")
	svc.LiveValue("Counter", lv)

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
	lv.Set(&Counter{Value: 1})
	lv.Set(&Counter{Value: 2})

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

func TestLiveValueHandler_ClosedLiveValue(t *testing.T) {
	lv := NewLiveValue("value")
	lv.Close()

	app := NewApp()
	svc := app.Service("System")
	svc.LiveValue("Status", lv)

	req := httptest.NewRequest("POST", "/System/Status", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should return error for closed livevalue
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
	if !strings.Contains(body, `"livevalue closed"`) {
		t.Errorf("expected message 'livevalue closed', got: %s", body)
	}
}

func TestLiveValueHandler_WithOptions(t *testing.T) {
	lv := NewLiveValue("value")

	authInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return nil, NewError(CodeUnauthenticated, "not authorized")
	}

	app := NewApp()
	svc := app.Service("System")
	svc.LiveValue("Status", lv,
		WithUnaryInterceptors(authInterceptor),
		WithStreamWriteTimeout(10*time.Second),
		WithStreamHeartbeat(30*time.Second),
	)

	req := httptest.NewRequest("POST", "/System/Status", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	// Should be rejected by interceptor
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLiveValue_SubscriberGetsLatestValue(t *testing.T) {
	// Test that slow subscribers get latest value (not queued intermediate values)
	lv := NewLiveValue(0)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var received []int
	started := make(chan struct{})
	var once sync.Once

	// Start a goroutine that sends updates continuously
	go func() {
		<-started
		for i := 1; i <= 1000; i++ {
			lv.Set(i)
			time.Sleep(time.Millisecond) // Spread out updates
		}
	}()

	for v := range lv.Subscribe(ctx) {
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
