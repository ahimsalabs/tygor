package tygor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"tygor.dev/internal"
)

// ErrStreamClosed is returned by StreamWriter.Send when the client has disconnected
// or the stream has been closed. Handlers should return when they receive this error.
var ErrStreamClosed = errors.New("stream closed")

// ErrWriteTimeout is returned by StreamWriter.Send when a write to the client timed out.
// This typically indicates a slow or unresponsive client.
var ErrWriteTimeout = errors.New("write timeout")

// StreamWriter sends events to a streaming client.
// It provides methods for sending events with optional SSE event IDs
// and for checking the client's last received event ID on reconnection.
//
// This interface enables testing stream handlers without a real HTTP connection:
//
//	type mockStreamWriter[T any] struct {
//	    events []T
//	}
//	func (m *mockStreamWriter[T]) Send(event T) error { m.events = append(m.events, event); return nil }
//	func (m *mockStreamWriter[T]) SendWithID(id string, event T) error { return m.Send(event) }
//	func (m *mockStreamWriter[T]) LastEventID() string { return "" }
type StreamWriter[T any] interface {
	// Send sends an event to the client.
	// Returns an error if the client has disconnected or the context is canceled.
	// All disconnect-related errors satisfy errors.Is(err, [ErrStreamClosed]).
	Send(event T) error

	// SendWithID sends an event with an SSE event ID.
	// The ID is sent as the "id:" field in the SSE stream, allowing clients
	// to resume from this point on reconnection via the Last-Event-ID header.
	SendWithID(id string, event T) error

	// LastEventID returns the client's Last-Event-ID header value.
	// This is set when the client reconnects after a disconnection.
	// Returns empty string on first connection or if the client didn't send the header.
	LastEventID() string
}

// streamSender is the concrete implementation of Stream used by the framework.
type streamSender[T any] struct {
	yieldAny    func(any, error) bool
	ctx         context.Context
	lastEventID string
}

// lastEventIDKey is the context key for passing Last-Event-ID to the Emitter.
type lastEventIDKey struct{}

// withLastEventID adds the Last-Event-ID to a context for use by Emitter.
func withLastEventID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, lastEventIDKey{}, id)
}

// getLastEventID retrieves the Last-Event-ID from context.
func getLastEventID(ctx context.Context) string {
	if id, ok := ctx.Value(lastEventIDKey{}).(string); ok {
		return id
	}
	return ""
}

// sseEvent wraps an event with an optional SSE event ID.
// Used internally to pass event IDs through the iterator chain.
type sseEvent struct {
	id    string
	event any
}

func (s *streamSender[T]) Send(event T) error {
	return s.sendWithOptionalID("", event)
}

func (s *streamSender[T]) SendWithID(id string, event T) error {
	return s.sendWithOptionalID(id, event)
}

func (s *streamSender[T]) sendWithOptionalID(id string, event T) error {
	select {
	case <-s.ctx.Done():
		return fmt.Errorf("%w: %w", ErrStreamClosed, s.ctx.Err())
	default:
	}

	// Wrap with ID if provided, otherwise send raw event
	var toYield any = event
	if id != "" {
		toYield = sseEvent{id: id, event: event}
	}

	if !s.yieldAny(toYield, nil) {
		return ErrStreamClosed
	}
	return nil
}

func (s *streamSender[T]) LastEventID() string {
	return s.lastEventID
}

// StreamHandler implements Endpoint for SSE streaming responses.
//
// Stream handlers return an iterator that yields events to the client.
// The connection stays open until the iterator is exhausted, an error occurs,
// or the client disconnects.
//
// Example:
//
//	func SubscribeToFeed(ctx context.Context, req *SubscribeRequest) iter.Seq2[*FeedEvent, error] {
//	    return func(yield func(*FeedEvent, error) bool) {
//	        ticker := time.NewTicker(time.Second)
//	        defer ticker.Stop()
//	        for {
//	            select {
//	            case <-ctx.Done():
//	                return
//	            case <-ticker.C:
//	                if !yield(&FeedEvent{Time: time.Now()}, nil) {
//	                    return
//	                }
//	            }
//	        }
//	    }
//	}
//
//	feed.Register("Subscribe", tygor.Stream(SubscribeToFeed))
type StreamHandler[Req any, Res any] struct {
	fn                 func(context.Context, Req) iter.Seq2[Res, error]
	fnAny              func(context.Context, Req) iter.Seq2[any, error] // for StreamEmit with event IDs
	unaryInterceptors  []UnaryInterceptor
	streamInterceptors []StreamInterceptor
	skipValidation     bool
	maxRequestBodySize *uint64
	writeTimeout       time.Duration
	heartbeatInterval  time.Duration
}

// streamIter2 creates a new SSE streaming handler from an iterator function.
// This is an internal API preserved for potential future use with iterator composition.
// For public use, prefer [Stream] which provides a simpler callback-based API.
func streamIter2[Req any, Res any](fn func(context.Context, Req) iter.Seq2[Res, error]) *StreamHandler[Req, Res] {
	return &StreamHandler[Req, Res]{
		fn: fn,
	}
}

// Stream creates a new SSE streaming handler from a callback function.
//
// The handler receives a [StreamWriter] to send events to the client.
// StreamWriter.Send returns an error when the stream should stop:
//   - Client disconnects
//   - Context is canceled or times out
//   - Write fails
//
// All disconnect-related errors satisfy errors.Is(err, [ErrStreamClosed]).
// For finer distinction, you can also check errors.Is(err, context.Canceled)
// or errors.Is(err, context.DeadlineExceeded).
//
// Handlers should return when Send returns an error. Any error returned
// by the handler (except [ErrStreamClosed]) is sent to the client as a final error event.
//
// Example:
//
//	func Subscribe(ctx context.Context, req *SubscribeRequest, stream tygor.StreamWriter[*FeedEvent]) error {
//	    // Check for reconnection
//	    if lastID := stream.LastEventID(); lastID != "" {
//	        // Resume from lastID
//	    }
//
//	    sub := broker.Subscribe(req.Topic)
//	    defer sub.Close()
//
//	    for {
//	        select {
//	        case <-ctx.Done():
//	            return nil
//	        case event := <-sub.Events():
//	            if err := stream.Send(event); err != nil {
//	                return err
//	            }
//	            // Or with event ID for reconnection support:
//	            // if err := stream.SendWithID(event.ID, event); err != nil { ... }
//	        }
//	    }
//	}
//
//	feed.Register("Subscribe", tygor.Stream(Subscribe))
func Stream[Req any, Res any](fn func(context.Context, Req, StreamWriter[Res]) error) *StreamHandler[Req, Res] {
	// Use fnAny to allow yielding sseEvent wrappers with event IDs
	iterFn := func(ctx context.Context, req Req) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			s := &streamSender[Res]{
				yieldAny:    yield,
				ctx:         ctx,
				lastEventID: getLastEventID(ctx),
			}

			err := fn(ctx, req, s)

			// Don't send ErrStreamClosed as an error event - it's expected
			if err != nil && !errors.Is(err, ErrStreamClosed) {
				yield(nil, err)
			}
		}
	}
	return &StreamHandler[Req, Res]{
		fnAny: iterFn,
	}
}

// WithUnaryInterceptor adds an interceptor that runs during stream setup.
// Unary interceptors execute before the stream starts, useful for auth checks.
// They do not see the stream response (it doesn't exist yet).
func (h *StreamHandler[Req, Res]) WithUnaryInterceptor(i UnaryInterceptor) *StreamHandler[Req, Res] {
	h.unaryInterceptors = append(h.unaryInterceptors, i)
	return h
}

// WithStreamInterceptor adds an interceptor that wraps the event stream.
// Stream interceptors can transform, filter, or observe events.
func (h *StreamHandler[Req, Res]) WithStreamInterceptor(i StreamInterceptor) *StreamHandler[Req, Res] {
	h.streamInterceptors = append(h.streamInterceptors, i)
	return h
}

// WithSkipValidation disables request validation for this handler.
func (h *StreamHandler[Req, Res]) WithSkipValidation() *StreamHandler[Req, Res] {
	h.skipValidation = true
	return h
}

// WithMaxRequestBodySize sets the maximum request body size for this handler.
func (h *StreamHandler[Req, Res]) WithMaxRequestBodySize(size uint64) *StreamHandler[Req, Res] {
	h.maxRequestBodySize = &size
	return h
}

// WithWriteTimeout sets the timeout for writing each event to the client.
// If a write takes longer than this duration, the stream is closed and
// emit returns [ErrWriteTimeout].
//
// A zero duration means no timeout (the default).
func (h *StreamHandler[Req, Res]) WithWriteTimeout(d time.Duration) *StreamHandler[Req, Res] {
	h.writeTimeout = d
	return h
}

// WithHeartbeat sets the interval for sending SSE heartbeat comments.
// This keeps connections alive through proxies with idle timeouts.
//
// Heartbeats are sent as SSE comments (": heartbeat\n\n") which are ignored
// by the EventSource API but reset idle timers on proxies.
//
// Default is 30 seconds. Use 0 to disable heartbeats.
func (h *StreamHandler[Req, Res]) WithHeartbeat(d time.Duration) *StreamHandler[Req, Res] {
	h.heartbeatInterval = d
	return h
}

// Metadata implements [Endpoint].
func (h *StreamHandler[Req, Res]) Metadata() *internal.MethodMetadata {
	var req Req
	var res Res
	return &internal.MethodMetadata{
		Primitive: "stream",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// metadata returns the runtime metadata for the stream handler.
func (h *StreamHandler[Req, Res]) metadata() *internal.MethodMetadata {
	return h.Metadata()
}

// serveHTTP implements the SSE streaming handler.
func (h *StreamHandler[Req, Res]) serveHTTP(ctx *rpcContext) {
	// 1. Decode request (same as ExecHandler)
	req, decodeErr := h.decodeRequest(ctx)
	if decodeErr != nil {
		handleError(ctx, decodeErr)
		return
	}

	// 2. Run unary interceptors for setup (auth, logging, etc.)
	// They don't see a response - just validate/reject the stream setup.
	setupErr := h.runSetupInterceptors(ctx, req)
	if setupErr != nil {
		handleError(ctx, setupErr)
		return
	}

	// 3. Set SSE headers and flush
	ctx.writer.Header().Set("Content-Type", "text/event-stream")
	ctx.writer.Header().Set("Cache-Control", "no-cache")
	ctx.writer.Header().Set("Connection", "keep-alive")
	ctx.writer.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// 4. Add Last-Event-ID to context for Emitter to access
	lastEventID := ctx.request.Header.Get("Last-Event-ID")
	ctxWithID := withLastEventID(ctx, lastEventID)

	// 5. Get the base iterator from the handler
	// Use fnAny if available (StreamEmit), otherwise wrap fn (Stream)
	var anyIter iter.Seq2[any, error]
	if h.fnAny != nil {
		anyIter = h.fnAny(ctxWithID, req)
	} else {
		baseIter := h.fn(ctxWithID, req)
		anyIter = func(yield func(any, error) bool) {
			for v, err := range baseIter {
				if !yield(v, err) {
					return
				}
			}
		}
	}

	// 6. Wrap with stream interceptors
	finalIter := h.wrapWithStreamInterceptors(ctx, req, anyIter)

	// 7. Stream events
	h.streamEvents(ctx, finalIter)
}

func (h *StreamHandler[Req, Res]) decodeRequest(ctx *rpcContext) (Req, error) {
	var req Req
	if ctx.request.Body != nil {
		effectiveLimit := ctx.maxRequestBodySize
		if h.maxRequestBodySize != nil {
			effectiveLimit = *h.maxRequestBodySize
		}
		if effectiveLimit > 0 {
			ctx.request.Body = http.MaxBytesReader(ctx.writer, ctx.request.Body, int64(effectiveLimit))
		}
		if err := json.NewDecoder(ctx.request.Body).Decode(&req); err != nil {
			// Empty body (EOF) is OK - treat as empty request ({})
			if !errors.Is(err, io.EOF) {
				return req, Errorf(CodeInvalidArgument, "failed to decode body: %v", err)
			}
		}
	}

	if !h.skipValidation {
		_, isEmptyType := any(req).(Empty)
		if !isEmptyType {
			if err := validate.Struct(req); err != nil {
				return req, err
			}
		}
	}
	return req, nil
}

func (h *StreamHandler[Req, Res]) runSetupInterceptors(ctx *rpcContext, req Req) error {
	// Combine all unary interceptors: global + service + handler
	allInterceptors := make([]UnaryInterceptor, 0, len(ctx.interceptors)+len(h.unaryInterceptors))
	allInterceptors = append(allInterceptors, ctx.interceptors...)
	allInterceptors = append(allInterceptors, h.unaryInterceptors...)

	if len(allInterceptors) == 0 {
		return nil
	}

	// Chain interceptors with a no-op final handler
	// We only care if they error out (reject the stream)
	chain := chainInterceptors(allInterceptors)
	noopHandler := func(ctx context.Context, req any) (any, error) {
		return nil, nil // Stream setup complete
	}

	_, err := chain(ctx, req, noopHandler)
	return err
}

func (h *StreamHandler[Req, Res]) wrapWithStreamInterceptors(ctx *rpcContext, req Req, anyIter iter.Seq2[any, error]) iter.Seq2[any, error] {
	// Combine stream interceptors: global + service + handler
	// For now, only handler-level stream interceptors are supported
	// TODO: Add service/global stream interceptors if needed
	allInterceptors := h.streamInterceptors

	if len(allInterceptors) == 0 {
		return anyIter
	}

	// Chain stream interceptors
	chain := chainStreamInterceptors(allInterceptors)
	finalHandler := func(ctx context.Context, req any) iter.Seq2[any, error] {
		return anyIter
	}

	return chain(ctx, req, finalHandler)
}

func (h *StreamHandler[Req, Res]) streamEvents(ctx *rpcContext, events iter.Seq2[any, error]) {
	flusher, ok := ctx.writer.(http.Flusher)
	if !ok {
		// Can't stream without flusher - send error and close
		handleError(ctx, NewError(CodeInternal, "streaming not supported"))
		return
	}

	// Flush headers immediately
	flusher.Flush()

	logger := ctx.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Determine effective write timeout: handler override > app default
	writeTimeout := ctx.streamWriteTimeout
	if h.writeTimeout > 0 {
		writeTimeout = h.writeTimeout
	}

	// Determine effective heartbeat interval: handler override > app default
	heartbeatInterval := ctx.streamHeartbeat
	if h.heartbeatInterval > 0 {
		heartbeatInterval = h.heartbeatInterval
	}

	// Check if underlying connection supports write deadlines
	// http.ResponseController provides access to SetWriteDeadline in Go 1.20+
	var rc *http.ResponseController
	if writeTimeout > 0 {
		rc = http.NewResponseController(ctx.writer)
	}

	// Channel to receive events from iterator goroutine
	type eventItem struct {
		event any
		err   error
	}
	eventCh := make(chan eventItem)
	done := make(chan struct{})
	defer close(done)

	// Run iterator in goroutine so we can interleave heartbeats
	go func() {
		defer close(eventCh)
		for event, err := range events {
			select {
			case eventCh <- eventItem{event, err}:
			case <-done:
				return
			}
		}
	}()

	// Set up heartbeat ticker if configured
	var heartbeat <-chan time.Time
	if heartbeatInterval > 0 {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		heartbeat = ticker.C
	}

	for {
		select {
		case <-ctx.request.Context().Done():
			return

		case <-heartbeat:
			// Send SSE comment as heartbeat
			if _, err := fmt.Fprint(ctx.writer, ": heartbeat\n\n"); err != nil {
				if !isClientDisconnect(err) {
					logger.Error("failed to write heartbeat",
						slog.String("endpoint", ctx.EndpointID()),
						slog.Any("error", err))
				}
				return
			}
			flusher.Flush()

		case item, ok := <-eventCh:
			if !ok {
				// Iterator exhausted - stream completed normally
				return
			}

			if item.err != nil {
				// Send error event and close stream
				h.writeSSEError(ctx.writer, item.err, logger)
				flusher.Flush()
				return
			}

			// Set write deadline if configured
			if rc != nil {
				if deadlineErr := rc.SetWriteDeadline(time.Now().Add(writeTimeout)); deadlineErr != nil {
					// SetWriteDeadline not supported - log once and continue without timeout
					logger.Warn("write deadline not supported",
						slog.String("endpoint", ctx.EndpointID()),
						slog.Any("error", deadlineErr))
					rc = nil // Don't try again
				}
			}

			// Write event
			if writeErr := h.writeSSEEvent(ctx.writer, item.event); writeErr != nil {
				// Distinguish client disconnect from actual errors
				if isClientDisconnect(writeErr) {
					logger.Debug("client disconnected during write",
						slog.String("endpoint", ctx.EndpointID()))
				} else {
					logger.Error("failed to write SSE event",
						slog.String("endpoint", ctx.EndpointID()),
						slog.Any("error", writeErr))
				}
				return
			}

			// Clear write deadline after successful write to prevent spurious timeouts
			if rc != nil {
				rc.SetWriteDeadline(time.Time{})
			}

			// Flush sends data to client immediately
			// Note: Flush() returns no error - failures surface on next Write()
			flusher.Flush()
		}
	}
}

// isClientDisconnect checks if an error indicates the client has disconnected.
func isClientDisconnect(err error) bool {
	if err == nil {
		return false
	}
	// Check for common disconnect errors
	errStr := err.Error()
	return errors.Is(err, context.Canceled) ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "client disconnected")
}

func (h *StreamHandler[Req, Res]) writeSSEEvent(w http.ResponseWriter, event any) error {
	// Check if event is wrapped with an ID
	var eventID string
	if evt, ok := event.(sseEvent); ok {
		eventID = evt.id
		event = evt.event
	}

	// Wrap in response envelope for consistency with unary calls
	envelope := response{Result: event}
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// Write event ID if present (for reconnection support)
	if eventID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", eventID); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func (h *StreamHandler[Req, Res]) writeSSEError(w http.ResponseWriter, err error, logger *slog.Logger) {
	svcErr := DefaultErrorTransformer(err)

	envelope := errorResponse{Error: svcErr}
	data, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		logger.Error("failed to marshal SSE error",
			slog.Any("original_error", err),
			slog.Any("marshal_error", marshalErr))
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// StreamHandlerFunc represents the next handler in a stream interceptor chain.
type StreamHandlerFunc func(ctx context.Context, req any) iter.Seq2[any, error]

// StreamInterceptor wraps stream execution.
//
// Unlike UnaryInterceptor which wraps a single request/response,
// StreamInterceptor wraps the entire event stream. It can:
//   - Transform or filter events
//   - Add logging for stream lifecycle
//   - Implement backpressure or rate limiting
//
// Example:
//
//	func loggingStreamInterceptor(ctx tygor.Context, req any, handler tygor.StreamHandlerFunc) iter.Seq2[any, error] {
//	    start := time.Now()
//	    events := handler(ctx, req)
//	    return func(yield func(any, error) bool) {
//	        count := 0
//	        for event, err := range events {
//	            count++
//	            if !yield(event, err) {
//	                break
//	            }
//	        }
//	        log.Printf("%s streamed %d events in %v", ctx.EndpointID(), count, time.Since(start))
//	    }
//	}
type StreamInterceptor func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error]

// chainStreamInterceptors combines multiple stream interceptors into one.
func chainStreamInterceptors(interceptors []StreamInterceptor) StreamInterceptor {
	if len(interceptors) == 0 {
		return nil
	}
	if len(interceptors) == 1 {
		return interceptors[0]
	}
	return func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error] {
		var chain StreamHandlerFunc = handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			current := interceptors[i]
			next := chain
			chain = func(c context.Context, r any) iter.Seq2[any, error] {
				tygorCtx, ok := c.(Context)
				if !ok {
					tygorCtx, _ = FromContext(c)
				}
				return current(tygorCtx, r, next)
			}
		}
		return chain(ctx, req)
	}
}
