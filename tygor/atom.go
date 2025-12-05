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

// Atom holds a single value that can be read, written, and subscribed to.
// Updates are broadcast to all subscribers via SSE streaming.
// Thread-safe for concurrent Get/Set operations.
//
// Unlike event streams, Atom represents current state - subscribers always
// receive the latest value, and intermediate updates may be skipped if
// a subscriber is slow.
//
// Example:
//
//	status := tygor.NewAtom(&Status{State: "idle"})
//
//	// Read current value
//	current := status.Get()
//
//	// Update and broadcast to all subscribers
//	status.Set(&Status{State: "running"})
//
//	// Register SSE endpoint with proper "atom" primitive
//	svc.Register("Status", status.Handler())
type Atom[T any] struct {
	mu          sync.RWMutex
	value       T
	bytes       json.RawMessage // pre-serialized for efficient broadcast
	subscribers map[int64]chan json.RawMessage
	nextSubID   int64
	closed      bool
}

// NewAtom creates a new Atom with the given initial value.
func NewAtom[T any](initial T) *Atom[T] {
	a := &Atom[T]{
		value:       initial,
		subscribers: make(map[int64]chan json.RawMessage),
	}
	// Pre-serialize initial value (ignore error - will be caught on first Set if invalid)
	a.bytes, _ = json.Marshal(initial)
	return a
}

// Get returns the current value.
func (a *Atom[T]) Get() T {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.value
}

// Set updates the value and broadcasts to all subscribers.
// The value is serialized once and the same bytes are sent to all subscribers.
// No-op if the Atom has been closed.
func (a *Atom[T]) Set(value T) {
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
func (a *Atom[T]) Update(fn func(T) T) {
	a.mu.Lock()
	newValue := fn(a.value)
	a.mu.Unlock()
	a.Set(newValue)
}

// Subscribe returns an iterator that yields the current value and all future
// updates until ctx is canceled or the Atom is closed.
// For use in Go code, not HTTP handlers.
func (a *Atom[T]) Subscribe(ctx context.Context) iter.Seq[T] {
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

// Handler returns an AtomHandler for registering with a Service.
// The handler uses the "atom" primitive for proper TypeScript codegen.
//
// Example:
//
//	svc.Register("Status", statusAtom.Handler())
func (a *Atom[T]) Handler() *AtomHandler[T] {
	return &AtomHandler[T]{atom: a}
}

// addSubscriber adds a channel to the subscriber list.
// Returns -1 if the Atom has been closed.
func (a *Atom[T]) addSubscriber(ch chan json.RawMessage) int64 {
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
func (a *Atom[T]) removeSubscriber(id int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.subscribers, id)
}

// Close signals all subscribers to disconnect and prevents new subscriptions.
// Safe to call multiple times. After Close, Set is a no-op.
func (a *Atom[T]) Close() {
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

// AtomHandler implements [Endpoint] for Atom subscriptions.
// It streams the current value immediately, then pushes updates via SSE.
type AtomHandler[T any] struct {
	atom              *Atom[T]
	interceptors      []UnaryInterceptor
	writeTimeout      time.Duration
	heartbeatInterval time.Duration
}

// WithUnaryInterceptor adds an interceptor that runs during stream setup.
func (h *AtomHandler[T]) WithUnaryInterceptor(i UnaryInterceptor) *AtomHandler[T] {
	h.interceptors = append(h.interceptors, i)
	return h
}

// WithWriteTimeout sets the timeout for writing each event to the client.
func (h *AtomHandler[T]) WithWriteTimeout(d time.Duration) *AtomHandler[T] {
	h.writeTimeout = d
	return h
}

// WithHeartbeat sets the interval for sending SSE heartbeat comments.
func (h *AtomHandler[T]) WithHeartbeat(d time.Duration) *AtomHandler[T] {
	h.heartbeatInterval = d
	return h
}

// Metadata implements [Endpoint].
func (h *AtomHandler[T]) Metadata() *internal.MethodMetadata {
	var req Empty
	var res T
	return &internal.MethodMetadata{
		Primitive: "atom",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// metadata returns the runtime metadata for the atom handler.
func (h *AtomHandler[T]) metadata() *internal.MethodMetadata {
	return h.Metadata()
}

// serveHTTP implements the SSE streaming for atom subscriptions.
func (h *AtomHandler[T]) serveHTTP(ctx *rpcContext) {
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

	// Check if atom is closed before setting up SSE
	h.atom.mu.RLock()
	current := h.atom.bytes
	closed := h.atom.closed
	h.atom.mu.RUnlock()

	if closed {
		handleError(ctx, NewError(CodeUnavailable, "atom closed"))
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
	if h.writeTimeout > 0 {
		writeTimeout = h.writeTimeout
	}
	heartbeatInterval := ctx.streamHeartbeat
	if h.heartbeatInterval > 0 {
		heartbeatInterval = h.heartbeatInterval
	}

	var rc *http.ResponseController
	if writeTimeout > 0 {
		rc = http.NewResponseController(ctx.writer)
	}

	if err := h.writeSSEEvent(ctx.writer, current); err != nil {
		if !isClientDisconnect(err) {
			logger.Error("failed to write initial atom value",
				slog.String("endpoint", ctx.EndpointID()),
				slog.Any("error", err))
		}
		return
	}
	flusher.Flush()

	// Subscribe for updates
	ch := make(chan json.RawMessage, 1)
	subID := h.atom.addSubscriber(ch)
	if subID < 0 {
		return // Atom was closed between check and subscribe
	}
	defer h.atom.removeSubscriber(subID)

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
				return // Atom was closed
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
					logger.Error("failed to write atom update",
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
func (h *AtomHandler[T]) writeSSEEvent(w http.ResponseWriter, data json.RawMessage) error {
	// Wrap in response envelope: {"result": <data>}
	// Since data is already JSON, we construct the envelope manually
	_, err := fmt.Fprintf(w, "data: {\"result\":%s}\n\n", data)
	return err
}
