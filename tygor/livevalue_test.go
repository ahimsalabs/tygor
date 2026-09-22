package tygor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type liveValueDecodeChecked int

func (value liveValueDecodeChecked) MarshalJSON() ([]byte, error) {
	if value < 0 {
		return []byte(`"not an integer"`), nil
	}
	return []byte(strconv.FormatInt(int64(value), 10)), nil
}

type liveValueMutatingDecoder struct {
	Value string
}

func (value liveValueMutatingDecoder) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.Value)
}

func (value *liveValueMutatingDecoder) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(bytes.Clone(data), &value.Value); err != nil {
		return err
	}
	for i := range data {
		data[i] = 'x'
	}
	return nil
}

type liveValueObservedWriter struct {
	*httptest.ResponseRecorder
	once         sync.Once
	onFirstWrite func()
}

func (w *liveValueObservedWriter) Write(data []byte) (int, error) {
	w.once.Do(w.onFirstWrite)
	return w.ResponseRecorder.Write(data)
}

func mustNewLiveValue[T any](t *testing.T, initial T) *LiveValue[T] {
	t.Helper()
	lv, err := NewLiveValue(initial)
	if err != nil {
		t.Fatalf("NewLiveValue() error = %v", err)
	}
	return lv
}

func TestLiveValue_GetSet(t *testing.T) {
	lv := mustNewLiveValue(t, 42)

	if got := lv.Get(); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	if err := lv.Set(100); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got := lv.Get(); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}
}

func TestLiveValue_Update(t *testing.T) {
	lv := mustNewLiveValue(t, 10)

	if err := lv.Update(func(v int) int {
		return v * 2
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got := lv.Get(); got != 20 {
		t.Errorf("expected 20, got %d", got)
	}
}

func TestLiveValue_UpdateIsAtomic(t *testing.T) {
	lv := mustNewLiveValue(t, 0)

	const (
		workers    = 8
		increments = 1_000
	)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range increments {
				if err := lv.Update(func(value int) int { return value + 1 }); err != nil {
					t.Errorf("Update() error = %v", err)
					return
				}
			}
		})
	}
	wg.Wait()

	if got, want := lv.Get(), workers*increments; got != want {
		t.Fatalf("Get() = %d, want %d", got, want)
	}
}

func TestLiveValue_SubscribeDoesNotMissUpdateDuringInitialYield(t *testing.T) {
	lv := mustNewLiveValue(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var values []int
	for value := range lv.Subscribe(ctx) {
		values = append(values, value)
		if len(values) == 1 {
			if err := lv.Set(2); err != nil {
				t.Fatalf("Set() error = %v", err)
			}
			continue
		}
		break
	}

	if len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("Subscribe() yielded %v, want [1 2]", values)
	}
}

func TestLiveValue_OwnsStateSnapshots(t *testing.T) {
	type state struct {
		Values map[string][]int `json:"values"`
	}

	initial := &state{Values: map[string][]int{"numbers": {1, 2}}}
	lv := mustNewLiveValue(t, initial)
	initial.Values["numbers"][0] = 9
	if got := lv.Get().Values["numbers"][0]; got != 1 {
		t.Fatalf("Get() after mutating constructor input = %d, want 1", got)
	}

	next := &state{Values: map[string][]int{"numbers": {3, 4}}}
	if err := lv.Set(next); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	next.Values["numbers"][0] = 9
	if got := lv.Get().Values["numbers"][0]; got != 3 {
		t.Fatalf("Get() after mutating Set input = %d, want 3", got)
	}

	got := lv.Get()
	got.Values["numbers"][0] = 9
	if got := lv.Get().Values["numbers"][0]; got != 3 {
		t.Fatalf("Get() after mutating prior result = %d, want 3", got)
	}

	for snapshot := range lv.Subscribe(context.Background()) {
		snapshot.Values["numbers"][0] = 9
		break
	}
	if got := lv.Get().Values["numbers"][0]; got != 3 {
		t.Fatalf("Get() after mutating Subscribe result = %d, want 3", got)
	}
}

func TestLiveValue_InvalidJSONDoesNotBecomeState(t *testing.T) {
	invalid, err := NewLiveValue(math.NaN())
	if err == nil || invalid != nil {
		t.Fatalf("NewLiveValue(NaN) = (%v, %v), want (nil, error)", invalid, err)
	}

	lv := mustNewLiveValue(t, 1.0)
	if err := lv.Set(math.NaN()); err == nil {
		t.Fatal("Set(NaN) error = nil, want serialization error")
	}
	if got := lv.Get(); got != 1 {
		t.Fatalf("Get() after Set(NaN) = %v, want 1", got)
	}

	decodeChecked := mustNewLiveValue(t, liveValueDecodeChecked(1))
	subID, _, updates, ok := decodeChecked.registerSubscriber()
	if !ok {
		t.Fatal("registerSubscriber() rejected open LiveValue")
	}
	defer decodeChecked.removeSubscriber(subID)

	if err := decodeChecked.Set(-1); err == nil {
		t.Fatal("Set() error = nil, want decode error")
	}
	if err := decodeChecked.Update(func(liveValueDecodeChecked) liveValueDecodeChecked { return -2 }); err == nil {
		t.Fatal("Update() error = nil, want decode error")
	}
	if got := decodeChecked.Get(); got != 1 {
		t.Fatalf("Get() after rejected updates = %d, want 1", got)
	}
	select {
	case data := <-updates:
		t.Fatalf("rejected update published %q", data)
	default:
	}
}

func TestLiveValue_CustomDecoderCannotMutateStoredJSON(t *testing.T) {
	lv := mustNewLiveValue(t, liveValueMutatingDecoder{Value: "safe"})

	for range 2 {
		if got := lv.Get().Value; got != "safe" {
			t.Fatalf("Get().Value = %q, want safe", got)
		}
	}
	if got, want := string(lv.bytes), `"safe"`; got != want {
		t.Fatalf("stored JSON = %q, want %q", got, want)
	}
}

func TestLiveValue_Subscribe(t *testing.T) {
	lv := mustNewLiveValue(t, "initial")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var values []string
	ready := make(chan struct{})
	secondReceived := make(chan struct{})
	done := make(chan struct{})

	go func() {
		for v := range lv.Subscribe(ctx) {
			values = append(values, v)
			switch len(values) {
			case 1:
				close(ready)
			case 2:
				close(secondReceived)
			case 3:
				cancel()
			}
		}
		close(done)
	}()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't receive initial value")
	}
	if err := lv.Set("second"); err != nil {
		t.Fatalf("Set(second) error = %v", err)
	}
	select {
	case <-secondReceived:
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't receive second value")
	}
	if err := lv.Set("third"); err != nil {
		t.Fatalf("Set(third) error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't complete")
	}

	if got, want := strings.Join(values, ","), "initial,second,third"; got != want {
		t.Errorf("Subscribe() yielded %q, want %q", got, want)
	}
}

func TestLiveValue_Close(t *testing.T) {
	lv := mustNewLiveValue(t, "value")

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

	// Updates should be rejected after close.
	if err := lv.Set("new value"); !errors.Is(err, ErrLiveValueClosed) {
		t.Fatalf("Set() error = %v, want ErrLiveValueClosed", err)
	}
	called := false
	if err := lv.Update(func(value string) string {
		called = true
		return value
	}); !errors.Is(err, ErrLiveValueClosed) {
		t.Fatalf("Update() error = %v, want ErrLiveValueClosed", err)
	}
	if called {
		t.Fatal("Update() called callback after Close")
	}
	if got := lv.Get(); got != "value" {
		t.Errorf("Get() after rejected updates = %q, want value", got)
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
	lv := mustNewLiveValue(t, 1)

	// Close multiple times should not panic
	lv.Close()
	lv.Close()
	lv.Close()
}

func TestLiveValue_ConcurrentAccess(t *testing.T) {
	lv := mustNewLiveValue(t, 0)

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOps = 100

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				if err := lv.Set(j); err != nil {
					t.Errorf("Set() error = %v", err)
					return
				}
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
				if err := lv.Update(func(v int) int { return v + 1 }); err != nil {
					t.Errorf("Update() error = %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()
}

func TestLiveValue_ConcurrentSetsPublishCurrentState(t *testing.T) {
	const (
		attempts        = 200
		subscriberCount = 32
	)
	for attempt := range attempts {
		lv := mustNewLiveValue(t, 0)
		updates := make([]<-chan json.RawMessage, 0, subscriberCount)
		for range subscriberCount {
			_, _, ch, ok := lv.registerSubscriber()
			if !ok {
				t.Fatal("registerSubscriber() rejected open LiveValue")
			}
			updates = append(updates, ch)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var firstErr, secondErr error
		wg.Go(func() {
			<-start
			firstErr = lv.Set(1)
		})
		wg.Go(func() {
			<-start
			secondErr = lv.Set(2)
		})
		close(start)
		wg.Wait()
		if firstErr != nil || secondErr != nil {
			t.Fatalf("attempt %d: Set errors = (%v, %v)", attempt, firstErr, secondErr)
		}

		current := lv.Get()
		for subscriber, ch := range updates {
			select {
			case data := <-ch:
				if got := mustDecodeLiveValue[int](data); got != current {
					t.Fatalf("attempt %d subscriber %d: pending state = %d, current = %d", attempt, subscriber, got, current)
				}
			default:
				t.Fatalf("attempt %d subscriber %d: no pending state", attempt, subscriber)
			}
		}
		lv.Close()
	}
}

func TestLiveValue_SetDoesNotRaceClose(t *testing.T) {
	const (
		attempts        = 300
		subscriberCount = 128
	)
	for range attempts {
		lv := mustNewLiveValue(t, 0)
		for range subscriberCount {
			if _, _, _, ok := lv.registerSubscriber(); !ok {
				t.Fatal("registerSubscriber() rejected open LiveValue")
			}
		}

		setDone := make(chan error, 1)
		go func() {
			setDone <- lv.Set(1)
		}()
		for lv.Get() != 1 {
			runtime.Gosched()
		}
		lv.Close()
		if err := <-setDone; err != nil {
			t.Fatalf("Set() error = %v", err)
		}
	}
}

func TestLiveValueHandler_Metadata(t *testing.T) {
	type Status struct {
		State string `json:"state"`
	}

	lv := mustNewLiveValue(t, &Status{State: "idle"})
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

	lv := mustNewLiveValue(t, &Status{State: "idle"})

	app := NewApp(WithStreamWriteTimeout(0))
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

func TestLiveValueHandler_DoesNotMissUpdateDuringInitialWrite(t *testing.T) {
	lv := mustNewLiveValue(t, 1)
	var updateErr error
	w := &liveValueObservedWriter{
		ResponseRecorder: httptest.NewRecorder(),
		onFirstWrite: func() {
			updateErr = lv.Set(2)
			lv.Close()
		},
	}

	app := NewApp(WithStreamWriteTimeout(0), WithStreamHeartbeat(0))
	svc := app.Service("System")
	svc.LiveValue("Value", lv)
	req := httptest.NewRequest("POST", "/System/Value", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	app.Handler().ServeHTTP(w, req)
	if updateErr != nil {
		t.Fatalf("Set() during initial write error = %v", updateErr)
	}

	body := w.Body.String()
	initialIndex := strings.Index(body, `data: {"result":1}`)
	updateIndex := strings.Index(body, `data: {"result":2}`)
	if initialIndex < 0 || updateIndex <= initialIndex {
		t.Fatalf("SSE body = %q, want initial state followed by update", body)
	}
}

func TestLiveValueHandler_SSE_Updates(t *testing.T) {
	type Counter struct {
		Value int `json:"value"`
	}

	lv := mustNewLiveValue(t, &Counter{Value: 0})

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
	if err := lv.Set(&Counter{Value: 1}); err != nil {
		t.Fatalf("Set(first update) error = %v", err)
	}
	if err := lv.Set(&Counter{Value: 2}); err != nil {
		t.Fatalf("Set(second update) error = %v", err)
	}

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
	lv := mustNewLiveValue(t, "value")
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
	lv := mustNewLiveValue(t, "value")

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
	lv := mustNewLiveValue(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var received []int
	for value := range lv.Subscribe(ctx) {
		received = append(received, value)
		if len(received) == 1 {
			for update := 1; update <= 1_000; update++ {
				if err := lv.Set(update); err != nil {
					t.Fatalf("Set(%d) error = %v", update, err)
				}
			}
			continue
		}
		break
	}

	if len(received) != 2 || received[0] != 0 || received[1] != 1_000 {
		t.Fatalf("Subscribe() yielded %v, want [0 1000]", received)
	}
}
