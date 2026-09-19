package tygor

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"time"

	"tygor.dev/internal"
)

// LiveValue holds a single value that can be read, written, and subscribed to.
// Updates are broadcast to all subscribers via SSE streaming.
// Thread-safe for concurrent Get/Set operations.
//
// Unlike event streams, LiveValue represents current state - subscribers always
// receive the latest value, and intermediate updates may be skipped if
// a subscriber is slow.
//
// Example:
//
//	status := tygor.NewLiveValue(&Status{State: "idle"})
//
//	// Read current value
//	current := status.Get()
//
//	// Update and broadcast to all subscribers
//	status.Set(&Status{State: "running"})
//
//	// Register SSE endpoint with proper "livevalue" primitive
//	svc.LiveValue("Status", status)
type LiveValue[T any] struct {
	mu          sync.RWMutex
	value       T
	bytes       json.RawMessage // pre-serialized for efficient broadcast
	subscribers map[int64]chan json.RawMessage
	nextSubID   int64
	closed      bool
}

// NewLiveValue creates a new LiveValue with the given initial value.
func NewLiveValue[T any](initial T) *LiveValue[T] {
	a := &LiveValue[T]{
		value:       initial,
		subscribers: make(map[int64]chan json.RawMessage),
	}
	// Pre-serialize initial value (ignore error - will be caught on first Set if invalid)
	a.bytes, _ = json.Marshal(initial)
	return a
}

// Get returns the current value.
func (a *LiveValue[T]) Get() T {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.value
}

// Set updates the value and broadcasts to all subscribers.
// The value is serialized once and the same bytes are sent to all subscribers.
// No-op if the LiveValue has been closed.
func (a *LiveValue[T]) Set(value T) {
	// Serialize once before taking lock
	data, err := json.Marshal(value)
	if err != nil {
		// If serialization fails, still update in-memory value
		// but don't broadcast (subscribers would get stale data)
		a.mu.Lock()
		if !a.closed {
			a.value = value
		}
		a.mu.Unlock()
		return
	}

	// Update value and snapshot subscribers
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.value = value
	a.bytes = data
	subs := make([]chan json.RawMessage, 0, len(a.subscribers))
	for _, ch := range a.subscribers {
		subs = append(subs, ch)
	}
	a.mu.Unlock()

	// Broadcast outside lock with non-blocking sends
	for _, ch := range subs {
		select {
		case ch <- data:
			// Delivered
		default:
			// Channel full - drain old value and send new (latest-wins)
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- data:
			default:
			}
		}
	}
}

// Update atomically applies fn to the current value.
// Useful for read-modify-write operations.
func (a *LiveValue[T]) Update(fn func(T) T) {
	a.mu.Lock()
	newValue := fn(a.value)
	a.mu.Unlock()
	a.Set(newValue)
}

// Subscribe returns an iterator that yields the current value and all future
// updates until ctx is canceled or the LiveValue is closed.
// For use in Go code, not HTTP handlers.
func (a *LiveValue[T]) Subscribe(ctx context.Context) iter.Seq[T] {
	return func(yield func(T) bool) {
		// Send current value
		a.mu.RLock()
		current := a.value
		closed := a.closed
		a.mu.RUnlock()

		if closed {
			return
		}

		if !yield(current) {
			return
		}

		// Create channel and subscribe
		ch := make(chan json.RawMessage, 1)
		subID := a.addSubscriber(ch)
		if subID < 0 {
			return // Atom was closed
		}
		defer a.removeSubscriber(subID)

		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-ch:
				if !ok {
					return // Atom was closed
				}
				var val T
				if err := json.Unmarshal(data, &val); err != nil {
					continue // Skip malformed data
				}
				if !yield(val) {
					return
				}
			}
		}
	}
}

func makeLiveValueHandler[T any](a *LiveValue[T], config liveValueConfig) *liveValueHandler[T] {
	return &liveValueHandler[T]{
		liveValue:         a,
		interceptors:      config.interceptors,
		writeTimeout:      config.writeTimeout,
		heartbeatInterval: config.heartbeatInterval,
	}
}

// addSubscriber adds a channel to the subscriber list.
// Returns -1 if the LiveValue has been closed.
func (a *LiveValue[T]) addSubscriber(ch chan json.RawMessage) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return -1
	}

	id := a.nextSubID
	a.nextSubID++
	a.subscribers[id] = ch
	return id
}

// removeSubscriber removes a channel from the subscriber list.
func (a *LiveValue[T]) removeSubscriber(id int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.subscribers, id)
}

// Close signals all subscribers to disconnect and prevents new subscriptions.
// Safe to call multiple times. After Close, Set is a no-op.
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

type liveValueHandler[T any] struct {
	liveValue         *LiveValue[T]
	interceptors      []UnaryInterceptor
	writeTimeout      *time.Duration
	heartbeatInterval *time.Duration
}

// metadata returns the runtime metadata for the livevalue handler.
func (h *liveValueHandler[T]) metadata() *internal.MethodMetadata {
	var req Empty
	var res T
	return &internal.MethodMetadata{
		Primitive: "livevalue",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// serveHTTP implements the SSE streaming for livevalue subscriptions.
func (h *liveValueHandler[T]) serveHTTP(ctx *rpcContext) {
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

	// Check if livevalue is closed before setting up SSE
	h.liveValue.mu.RLock()
	current := h.liveValue.bytes
	closed := h.liveValue.closed
	h.liveValue.mu.RUnlock()

	if closed {
		handleError(ctx, NewError(CodeUnavailable, "livevalue closed"))
		return
	}

	// Set SSE headers
	ctx.writer.Header().Set("Content-Type", "text/event-stream")
	ctx.writer.Header().Set("Cache-Control", "no-cache")
	ctx.writer.Header().Set("Connection", "keep-alive")
	ctx.writer.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := ctx.writer.(http.Flusher)
	if !ok {
		handleError(ctx, NewError(CodeInternal, "streaming not supported"))
		return
	}
	flusher.Flush()

	logger := ctx.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Determine effective timeouts
	writeTimeout := ctx.streamWriteTimeout
	if h.writeTimeout != nil {
		writeTimeout = *h.writeTimeout
	}
	heartbeatInterval := ctx.streamHeartbeat
	if h.heartbeatInterval != nil {
		heartbeatInterval = *h.heartbeatInterval
	}

	var rc *http.ResponseController
	if writeTimeout > 0 {
		rc = http.NewResponseController(ctx.writer)
	}

	if err := h.writeSSEEvent(ctx.writer, current); err != nil {
		if !isClientDisconnect(err) {
			logger.Error("failed to write initial livevalue value",
				slog.String("endpoint", ctx.EndpointID()),
				slog.Any("error", err))
		}
		return
	}
	flusher.Flush()

	// Subscribe for updates
	ch := make(chan json.RawMessage, 1)
	subID := h.liveValue.addSubscriber(ch)
	if subID < 0 {
		return // LiveValue was closed between check and subscribe
	}
	defer h.liveValue.removeSubscriber(subID)

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
			if _, err := fmt.Fprint(ctx.writer, ": heartbeat\n\n"); err != nil {
				if !isClientDisconnect(err) {
					logger.Error("failed to write heartbeat",
						slog.String("endpoint", ctx.EndpointID()),
						slog.Any("error", err))
				}
				return
			}
			flusher.Flush()

		case data, ok := <-ch:
			if !ok {
				return // LiveValue was closed
			}

			if rc != nil {
				if err := rc.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
					logger.Warn("write deadline not supported",
						slog.String("endpoint", ctx.EndpointID()),
						slog.Any("error", err))
					rc = nil
				}
			}

			if err := h.writeSSEEvent(ctx.writer, data); err != nil {
				if !isClientDisconnect(err) {
					logger.Error("failed to write livevalue update",
						slog.String("endpoint", ctx.EndpointID()),
						slog.Any("error", err))
				}
				return
			}

			if rc != nil {
				rc.SetWriteDeadline(time.Time{})
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes a pre-serialized value as an SSE event.
func (h *liveValueHandler[T]) writeSSEEvent(w http.ResponseWriter, data json.RawMessage) error {
	// Wrap in response envelope: {"result": <data>}
	// Since data is already JSON, we construct the envelope manually
	_, err := fmt.Fprintf(w, "data: {\"result\":%s}\n\n", data)
	return err
}
