package tygor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"time"

	"tygor.dev/internal"
)

// ErrLiveValueClosed is returned when an update is attempted after Close.
var ErrLiveValueClosed = errors.New("livevalue closed")

// LiveValue holds a single value that can be read, written, and subscribed to.
// Updates are broadcast to all subscribers via SSE streaming.
// All operations are safe for concurrent use.
//
// Unlike event streams, LiveValue represents current state - subscribers always
// receive the latest value, and intermediate updates may be skipped if
// a subscriber is slow.
//
// LiveValue owns its state as JSON. Values must round-trip through encoding/json.
// Get and Subscribe decode fresh snapshots, so callers may safely mutate returned
// values. Go state that JSON does not represent, such as unexported fields and
// pointer identity, is not preserved, and interface values use encoding/json's
// default decoded types. Custom JSON codecs must decode deterministically. Inputs
// may be mutated after NewLiveValue, Set, or Update returns, but not concurrently
// while that operation is encoding them.
//
// Example:
//
//	status, err := tygor.NewLiveValue(&Status{State: "idle"})
//	if err != nil {
//		return err
//	}
//
//	// Read current value
//	current := status.Get()
//
//	// Update and broadcast to all subscribers
//	if err := status.Set(&Status{State: "running"}); err != nil {
//		return err
//	}
//
//	// Register SSE endpoint with proper "livevalue" primitive
//	svc.Register("Status", status.Handler())
type LiveValue[T any] struct {
	mu          sync.RWMutex
	bytes       json.RawMessage
	subscribers map[int64]chan json.RawMessage
	nextSubID   int64
	closed      bool
}

// NewLiveValue creates a LiveValue whose initial state is an owned JSON snapshot.
// It returns an error if initial cannot round-trip through encoding/json.
func NewLiveValue[T any](initial T) (*LiveValue[T], error) {
	data, err := encodeLiveValue(initial)
	if err != nil {
		return nil, fmt.Errorf("initialize livevalue: %w", err)
	}

	return &LiveValue[T]{
		bytes:       data,
		subscribers: make(map[int64]chan json.RawMessage),
	}, nil
}

// Get returns a fresh decode of the current JSON snapshot.
func (a *LiveValue[T]) Get() T {
	a.mu.RLock()
	data := a.bytes
	a.mu.RUnlock()
	return mustDecodeLiveValue[T](data)
}

// Set updates the value and broadcasts to all subscribers.
// The update is rejected without changing state if value cannot round-trip
// through encoding/json. It returns ErrLiveValueClosed after Close.
func (a *LiveValue[T]) Set(value T) error {
	data, err := encodeLiveValue(value)
	if err != nil {
		return fmt.Errorf("set livevalue: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrLiveValueClosed
	}
	a.commitLocked(data)
	return nil
}

// Update atomically applies fn to the current value.
// The callback is invoked exactly once while the LiveValue is locked. It must be
// short-running and must not call methods on this LiveValue. The update is
// rejected without changing state if its result cannot round-trip through JSON.
func (a *LiveValue[T]) Update(fn func(T) T) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrLiveValueClosed
	}

	current := mustDecodeLiveValue[T](a.bytes)
	data, err := encodeLiveValue(fn(current))
	if err != nil {
		return fmt.Errorf("update livevalue: %w", err)
	}
	a.commitLocked(data)
	return nil
}

// Subscribe returns an iterator that yields the current value and all future
// updates until ctx is canceled or the LiveValue is closed.
// For use in Go code, not HTTP handlers.
func (a *LiveValue[T]) Subscribe(ctx context.Context) iter.Seq[T] {
	return func(yield func(T) bool) {
		subID, current, ch, ok := a.registerSubscriber()
		if !ok {
			return
		}
		defer a.removeSubscriber(subID)

		if !yield(mustDecodeLiveValue[T](current)) {
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-ch:
				if !ok {
					return
				}
				if !yield(mustDecodeLiveValue[T](data)) {
					return
				}
			}
		}
	}
}

// Handler returns a LiveValueHandler for registering with a Service.
// The handler uses the "livevalue" primitive for proper TypeScript codegen.
//
// Example:
//
//	svc.Register("Status", statusLiveValue.Handler())
func (a *LiveValue[T]) Handler() *LiveValueHandler[T] {
	return &LiveValueHandler[T]{liveValue: a}
}

func (a *LiveValue[T]) isClosed() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.closed
}

// registerSubscriber atomically captures the initial state and registers for
// every later state. The caller must remove a successful registration.
func (a *LiveValue[T]) registerSubscriber() (int64, json.RawMessage, <-chan json.RawMessage, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return 0, nil, nil, false
	}

	ch := make(chan json.RawMessage, 1)
	id := a.nextSubID
	a.nextSubID++
	a.subscribers[id] = ch
	return id, a.bytes, ch, true
}

// removeSubscriber removes a channel from the subscriber list.
func (a *LiveValue[T]) removeSubscriber(id int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.subscribers, id)
}

// commitLocked replaces the current state and queues it for every subscriber.
// a.mu must be held by the caller. Queue capacity is one: when full, the older
// pending state is replaced so slow subscribers eventually observe the latest.
func (a *LiveValue[T]) commitLocked(data json.RawMessage) {
	a.bytes = data
	for _, ch := range a.subscribers {
		select {
		case ch <- data:
			continue
		default:
		}

		select {
		case <-ch:
		default:
		}
		// The channel has capacity one and every producer holds a.mu, so the
		// drain above guarantees room even if the consumer receives concurrently.
		ch <- data
	}
}

// Close signals all subscribers to disconnect and prevents new subscriptions.
// Safe to call multiple times. After Close, Set and Update return
// ErrLiveValueClosed, while Get continues to return the last accepted state.
func (a *LiveValue[T]) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return
	}
	a.closed = true

	// Close all subscriber channels to signal disconnection
	for id, ch := range a.subscribers {
		close(ch)
		delete(a.subscribers, id)
	}
}

func encodeLiveValue[T any](value T) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON snapshot: %w", err)
	}
	if _, err := decodeLiveValue[T](data); err != nil {
		return nil, fmt.Errorf("decode JSON snapshot: %w", err)
	}
	return data, nil
}

func decodeLiveValue[T any](data json.RawMessage) (T, error) {
	var value T
	if err := json.Unmarshal(bytes.Clone(data), &value); err != nil {
		return value, err
	}
	return value, nil
}

func mustDecodeLiveValue[T any](data json.RawMessage) T {
	value, err := decodeLiveValue[T](data)
	if err != nil {
		// Every stored snapshot was already decoded successfully. A later failure
		// means a custom codec violated LiveValue's deterministic codec contract.
		panic(fmt.Sprintf("tygor: decode validated livevalue snapshot: %v", err))
	}
	return value
}

// LiveValueHandler implements [Endpoint] for LiveValue subscriptions.
// It streams the current value immediately, then pushes updates via SSE.
type LiveValueHandler[T any] struct {
	liveValue         *LiveValue[T]
	interceptors      []UnaryInterceptor
	writeTimeout      time.Duration
	writeTimeoutIsSet bool
	heartbeatInterval time.Duration
}

// WithUnaryInterceptor adds an interceptor that runs during stream setup.
func (h *LiveValueHandler[T]) WithUnaryInterceptor(i UnaryInterceptor) *LiveValueHandler[T] {
	h.interceptors = append(h.interceptors, i)
	return h
}

// WithWriteTimeout sets the timeout for writing each event to the client.
// A zero duration explicitly disables deadlines but still requires flushing.
func (h *LiveValueHandler[T]) WithWriteTimeout(d time.Duration) *LiveValueHandler[T] {
	h.writeTimeout = d
	h.writeTimeoutIsSet = true
	return h
}

// WithHeartbeat sets the interval for sending SSE heartbeat comments.
func (h *LiveValueHandler[T]) WithHeartbeat(d time.Duration) *LiveValueHandler[T] {
	h.heartbeatInterval = d
	return h
}

// Metadata implements [Endpoint].
func (h *LiveValueHandler[T]) Metadata() *internal.MethodMetadata {
	var req Empty
	var res T
	return &internal.MethodMetadata{
		Primitive: "livevalue",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// metadata returns the runtime metadata for the livevalue handler.
func (h *LiveValueHandler[T]) metadata() *internal.MethodMetadata {
	return h.Metadata()
}

// serveHTTP implements the SSE streaming for livevalue subscriptions.
func (h *LiveValueHandler[T]) serveHTTP(ctx *rpcContext) {
	state := &streamResponseState{}
	logger := ctx.logger
	if logger == nil {
		logger = slog.Default()
	}
	defer func() {
		if rec := recover(); rec != nil {
			if state.started {
				ctx.panicRecovery.claim()
			}
			panic(rec)
		}
	}()

	// Run unary interceptors for setup (auth, etc.)
	if len(h.interceptors) > 0 || len(ctx.interceptors) > 0 {
		allInterceptors := make([]UnaryInterceptor, 0, len(ctx.interceptors)+len(h.interceptors))
		allInterceptors = append(allInterceptors, ctx.interceptors...)
		allInterceptors = append(allInterceptors, h.interceptors...)

		chain := chainInterceptors(allInterceptors)
		noopHandler := func(ctx context.Context, req any) (any, error) {
			return nil, nil
		}
		if _, err := chain(ctx, nil, noopHandler); err != nil {
			handleError(ctx, err)
			return
		}
	}
	if h.liveValue.isClosed() {
		handleError(ctx, NewError(CodeUnavailable, "livevalue closed"))
		return
	}

	writeTimeout := ctx.streamWriteTimeout
	if h.writeTimeoutIsSet {
		writeTimeout = h.writeTimeout
	}
	var preflightErr error
	ctx.panicRecovery.own(func() {
		preflightErr = preflightSSE(ctx.writer, writeTimeout)
	})
	if preflightErr != nil {
		logger.Error("livevalue preflight failed",
			slog.String("endpoint", ctx.EndpointID()),
			slog.Any("error", preflightErr))
		handleError(ctx, NewError(CodeInternal, "streaming response capabilities not supported"))
		return
	}

	// Capture the initial state and register before any network I/O so updates
	// during the initial write are queued rather than lost.
	subID, current, ch, ok := h.liveValue.registerSubscriber()
	if !ok {
		handleError(ctx, NewError(CodeUnavailable, "livevalue closed"))
		return
	}
	defer h.liveValue.removeSubscriber(subID)

	// Set SSE headers
	ctx.panicRecovery.own(func() {
		ctx.writer.Header().Set("Content-Type", "text/event-stream")
		ctx.writer.Header().Set("Cache-Control", "no-cache")
		ctx.writer.Header().Set("Connection", "keep-alive")
		ctx.writer.Header().Set("X-Accel-Buffering", "no")
	})

	heartbeatInterval := ctx.streamHeartbeat
	if h.heartbeatInterval > 0 {
		heartbeatInterval = h.heartbeatInterval
	}

	abortTransport := func(message string, err error) {
		if !isClientDisconnect(err) {
			logger.Error(message,
				slog.String("endpoint", ctx.EndpointID()),
				slog.Any("error", err))
		}
		ctx.panicRecovery.claim()
		panic(http.ErrAbortHandler)
	}

	if err := state.writeFrame(ctx, writeTimeout, nil); err != nil {
		if !state.started {
			handleError(ctx, NewError(CodeInternal, "streaming response setup failed"))
			return
		}
		abortTransport("failed to flush livevalue headers", err)
	}

	if err := state.writeFrame(ctx, writeTimeout, liveValueSSEFrame(current)); err != nil {
		abortTransport("failed to write initial livevalue value", err)
	}

	// Set up heartbeat
	var heartbeat <-chan time.Time
	if heartbeatInterval > 0 {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		heartbeat = ticker.C
	}

	// Stream updates until disconnect
	for {
		select {
		case <-ctx.request.Context().Done():
			return

		case <-heartbeat:
			if err := state.writeFrame(ctx, writeTimeout, []byte(": heartbeat\n\n")); err != nil {
				abortTransport("failed to write heartbeat", err)
			}

		case data, ok := <-ch:
			if !ok {
				return // LiveValue was closed
			}

			if err := state.writeFrame(ctx, writeTimeout, liveValueSSEFrame(data)); err != nil {
				abortTransport("failed to write livevalue update", err)
			}
		}
	}
}

func liveValueSSEFrame(data json.RawMessage) []byte {
	frame := make([]byte, 0, len(data)+len("data: {\"result\":}\n\n"))
	frame = append(frame, "data: {\"result\":"...)
	frame = append(frame, data...)
	return append(frame, "}\n\n"...)
}
