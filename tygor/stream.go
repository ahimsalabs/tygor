package tygor

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"tygor.dev/internal"
)

// ErrStreamClosed is returned by StreamWriter.Send when the client has disconnected
// or the stream has been closed. Handlers should return when they receive this error.
var ErrStreamClosed = errors.New("stream closed")

// ErrWriteTimeout is returned by StreamWriter.Send when a write to the client timed out.
// This typically indicates a slow or unresponsive client.
var ErrWriteTimeout = errors.New("write timeout")

// ErrInvalidStreamEventID is returned by StreamWriter.SendWithID when an event
// ID contains characters that can alter SSE framing.
var ErrInvalidStreamEventID = errors.New("invalid stream event ID")

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
	// Send synchronously submits an event to the interceptor pipeline. For events
	// forwarded synchronously, it waits for serialization, writing, and flushing.
	// Interceptors may transform, filter, or buffer an event, so a nil error does
	// not guarantee a corresponding frame reached the client.
	//
	// All stream-closure errors satisfy errors.Is(err, [ErrStreamClosed]).
	Send(event T) error

	// SendWithID sends an event with an SSE event ID.
	// The ID is sent as the "id:" field in the SSE stream, allowing clients
	// to resume from this point on reconnection via the Last-Event-ID header.
	// IDs containing carriage return, line feed, or NUL are rejected with
	// [ErrInvalidStreamEventID].
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
	session     *streamSession
	lastEventID string
}

type streamSessionKey struct{}

type streamSession struct {
	ctx    context.Context
	cancel context.CancelCauseFunc

	mu    sync.Mutex
	cause error
}

func newStreamSession(parent context.Context) *streamSession {
	ctx, cancel := context.WithCancelCause(parent)
	return &streamSession{ctx: ctx, cancel: cancel}
}

func (s *streamSession) fail(err error) error {
	if err == nil {
		err = ErrStreamClosed
	}
	if !errors.Is(err, ErrStreamClosed) {
		err = fmt.Errorf("%w: %w", ErrStreamClosed, err)
	}

	shouldCancel := false
	s.mu.Lock()
	if s.cause == nil {
		s.cause = err
		shouldCancel = true
	}
	cause := s.cause
	s.mu.Unlock()
	if shouldCancel {
		s.cancel(err)
	}
	return cause
}

func (s *streamSession) terminalCause() error {
	s.mu.Lock()
	cause := s.cause
	s.mu.Unlock()
	if cause != nil {
		return cause
	}
	if cause = context.Cause(s.ctx); cause != nil && !errors.Is(cause, ErrStreamClosed) {
		return fmt.Errorf("%w: %w", ErrStreamClosed, cause)
	}
	return cause
}

func withStreamSession(ctx context.Context, session *streamSession) context.Context {
	return context.WithValue(ctx, streamSessionKey{}, session)
}

func streamSessionFromContext(ctx context.Context) *streamSession {
	session, _ := ctx.Value(streamSessionKey{}).(*streamSession)
	return session
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
	return s.sendWithOptionalID("", false, event)
}

func (s *streamSender[T]) SendWithID(id string, event T) error {
	if err := validateSSEEventID(id); err != nil {
		return err
	}
	return s.sendWithOptionalID(id, true, event)
}

func (s *streamSender[T]) sendWithOptionalID(id string, hasID bool, event T) error {
	if s.session != nil {
		if cause := s.session.terminalCause(); cause != nil {
			return cause
		}
	}
	select {
	case <-s.ctx.Done():
		return fmt.Errorf("%w: %w", ErrStreamClosed, s.ctx.Err())
	default:
	}

	// Wrap when SendWithID was used, including an empty ID. An explicit empty
	// SSE id field resets the client's last-event-ID state.
	var toYield any = event
	if hasID {
		toYield = sseEvent{id: id, event: event}
	}

	if !s.yieldAny(toYield, nil) {
		if s.session != nil {
			if cause := s.session.terminalCause(); cause != nil {
				return cause
			}
		}
		return ErrStreamClosed
	}
	return nil
}

func (s *streamSender[T]) LastEventID() string {
	return s.lastEventID
}

type streamHandler[Req any, Res any] struct {
	fn                 func(context.Context, Req) iter.Seq2[Res, error]
	fnAny              func(context.Context, Req) iter.Seq2[any, error] // for StreamEmit with event IDs
	unaryInterceptors  []UnaryInterceptor
	streamInterceptors []StreamInterceptor
	skipValidation     bool
	maxRequestBodySize *uint64
	writeTimeout       *time.Duration
	heartbeatInterval  *time.Duration
}

// streamIter2 creates a new SSE streaming handler from an iterator function.
// This is an internal API preserved for potential future use with iterator composition.
// The public [Service.Stream] method provides a simpler callback-based API.
func streamIter2[Req any, Res any](fn func(context.Context, Req) iter.Seq2[Res, error], options ...StreamOption) *streamHandler[Req, Res] {
	config := streamConfig{}
	for _, option := range options {
		option.applyStream(&config)
	}
	return &streamHandler[Req, Res]{
		fn:                 fn,
		unaryInterceptors:  config.unaryInterceptors,
		streamInterceptors: config.streamInterceptors,
		skipValidation:     config.skipValidation,
		maxRequestBodySize: config.maxRequestBodySize,
		writeTimeout:       config.writeTimeout,
		heartbeatInterval:  config.heartbeatInterval,
	}
}

func makeStreamHandler[Req any, Res any](fn func(context.Context, Req, StreamWriter[Res]) error, config streamConfig) *streamHandler[Req, Res] {
	// Use fnAny to allow yielding sseEvent wrappers with event IDs
	iterFn := func(ctx context.Context, req Req) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			s := &streamSender[Res]{
				yieldAny:    yield,
				ctx:         ctx,
				session:     streamSessionFromContext(ctx),
				lastEventID: getLastEventID(ctx),
			}

			err := fn(ctx, req, s)

			// Don't send ErrStreamClosed as an error event - it's expected
			if err != nil && !errors.Is(err, ErrStreamClosed) && s.session.terminalCause() == nil {
				yield(nil, err)
			}
		}
	}
	return &streamHandler[Req, Res]{
		fnAny:              iterFn,
		unaryInterceptors:  config.unaryInterceptors,
		streamInterceptors: config.streamInterceptors,
		skipValidation:     config.skipValidation,
		maxRequestBodySize: config.maxRequestBodySize,
		writeTimeout:       config.writeTimeout,
		heartbeatInterval:  config.heartbeatInterval,
	}
}

// metadata returns the runtime metadata for the stream handler.
func (h *streamHandler[Req, Res]) metadata() *internal.MethodMetadata {
	var req Req
	var res Res
	return &internal.MethodMetadata{
		Primitive: "stream",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// serveHTTP implements the SSE streaming handler.
func (h *streamHandler[Req, Res]) serveHTTP(ctx *rpcContext) {
	state := &streamResponseState{}
	defer h.recoverStreamPanic(ctx, state)

	req, decodeErr := h.decodeRequest(ctx)
	if decodeErr != nil {
		h.writeUnaryError(ctx, state, decodeErr)
		return
	}
	var preflightErr error
	ctx.panicRecovery.own(func() {
		preflightErr = preflightSSE(ctx.writer, h.effectiveWriteTimeout(ctx))
	})
	if preflightErr != nil {
		h.logStreamFailure(ctx, "stream preflight failed", preflightErr)
		h.writeUnaryError(ctx, state, NewError(CodeInternal, "streaming response capabilities not supported"))
		return
	}

	err := h.executeStream(ctx, req, state)
	if state.handlerPanic != nil {
		panic(state.handlerPanic)
	}
	if state.transportActive || state.transportFailed {
		if err != nil {
			h.logStreamFailure(ctx, "stream transport failed", err)
		}
		panic(http.ErrAbortHandler)
	}
	if state.serializationErr != nil {
		h.logStreamFailure(ctx, "failed to marshal stream event", state.serializationErr)
		err = state.serializationErr
	} else if err == nil || errors.Is(err, ErrStreamClosed) {
		return
	}

	if !state.started {
		h.writeUnaryError(ctx, state, err)
		return
	}

	if writeErr := h.writeTerminalSSEError(ctx, state, err); writeErr != nil {
		h.logStreamFailure(ctx, "failed to write SSE error", writeErr)
		panic(http.ErrAbortHandler)
	}
}

type streamResponseState struct {
	started          bool
	transportFailed  bool
	transportActive  bool
	terminalAttempt  bool
	handlerPanic     any
	serializationErr error
}

func (s *streamResponseState) writeFrame(ctx *rpcContext, timeout time.Duration, frame []byte) error {
	if s.transportActive || s.transportFailed {
		s.transportFailed = true
		return ErrStreamClosed
	}

	s.transportActive = true
	var err error
	ctx.panicRecovery.own(func() {
		err = writeSSEFrameWithCommit(ctx.writer, timeout, frame, func() {
			s.started = true
		})
	})
	s.transportActive = false
	if err != nil && s.started {
		s.transportFailed = true
	}
	return err
}

func (h *streamHandler[Req, Res]) recoverStreamPanic(ctx *rpcContext, state *streamResponseState) {
	rec := recover()
	if rec == nil {
		return
	}
	transportOwned := ctx.panicRecovery.isOwned()
	ctx.panicRecovery.claim()
	if transportOwned || state.transportActive || state.transportFailed || state.terminalAttempt {
		panic(rec)
	}

	logPanic(ctx.logger, ctx.EndpointID(), "stream handler", rec)
	panicErr := NewError(CodeInternal, "internal server error")
	if !state.started {
		h.writeUnaryError(ctx, state, panicErr)
		return
	}
	if err := h.writeTerminalSSEError(ctx, state, panicErr); err != nil {
		h.logStreamFailure(ctx, "failed to write SSE panic error", err)
		panic(http.ErrAbortHandler)
	}
}

func (h *streamHandler[Req, Res]) writeUnaryError(ctx *rpcContext, state *streamResponseState, err error) {
	state.terminalAttempt = true
	handleError(ctx, err)
}

func (h *streamHandler[Req, Res]) writeTerminalSSEError(ctx *rpcContext, state *streamResponseState, err error) error {
	state.terminalAttempt = true
	frame := marshalSSEErrorFrame(ctx, err)
	return state.writeFrame(ctx, h.effectiveWriteTimeout(ctx), frame)
}

type streamTransportError struct{ err error }

func (e *streamTransportError) Error() string { return e.err.Error() }
func (e *streamTransportError) Unwrap() error { return e.err }

func (h *streamHandler[Req, Res]) executeStream(ctx *rpcContext, req Req, state *streamResponseState) error {
	allInterceptors := make([]UnaryInterceptor, 0, len(ctx.interceptors)+len(h.unaryInterceptors))
	allInterceptors = append(allInterceptors, ctx.interceptors...)
	allInterceptors = append(allInterceptors, h.unaryInterceptors...)
	chain := chainInterceptors(allInterceptors)
	if chain != nil {
		if _, err := chain(ctx, req, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
			return err
		}
	}
	return h.runStream(ctx, ctx.request.Context(), req, state)
}

func (h *streamHandler[Req, Res]) decodeRequest(ctx *rpcContext) (Req, error) {
	var req Req
	if ctx.request.Body != nil {
		effectiveLimit := ctx.maxRequestBodySize
		if h.maxRequestBodySize != nil {
			effectiveLimit = *h.maxRequestBodySize
		}
		if effectiveLimit > 0 {
			ctx.request.Body = http.MaxBytesReader(ctx.writer, ctx.request.Body, int64(effectiveLimit))
		}
		if err := decodeJSONBody(ctx.request.Body, &req); err != nil {
			return req, Errorf(CodeInvalidArgument, "failed to decode body: %v", err)
		}
	}

	if !h.skipValidation {
		if err := validateRequest(req); err != nil {
			return req, err
		}
	}
	return req, nil
}

func (h *streamHandler[Req, Res]) effectiveWriteTimeout(ctx *rpcContext) time.Duration {
	if h.writeTimeout != nil {
		return *h.writeTimeout
	}
	return ctx.streamWriteTimeout
}

func (h *streamHandler[Req, Res]) effectiveHeartbeat(ctx *rpcContext) time.Duration {
	if h.heartbeatInterval != nil {
		return *h.heartbeatInterval
	}
	return ctx.streamHeartbeat
}

func (h *streamHandler[Req, Res]) runStream(ctx *rpcContext, executionCtx context.Context, req Req, state *streamResponseState) error {
	session := newStreamSession(executionCtx)
	defer session.cancel(ErrStreamClosed)

	baseHandler := func(sourceCtx context.Context, reqAny any) iter.Seq2[any, error] {
		reqTyped, ok := reqAny.(Req)
		if !ok {
			return func(yield func(any, error) bool) {
				yield(nil, Errorf(CodeInternal, "stream interceptor modified request type incorrectly"))
			}
		}

		sourceCtx = withStreamSession(sourceCtx, session)
		sourceCtx = withLastEventID(sourceCtx, ctx.request.Header.Get("Last-Event-ID"))
		if h.fnAny != nil {
			return h.fnAny(sourceCtx, reqTyped)
		}

		baseIter := h.fn(sourceCtx, reqTyped)
		return func(yield func(any, error) bool) {
			for event, err := range baseIter {
				if !yield(event, err) {
					return
				}
			}
		}
	}

	var events iter.Seq2[any, error]
	baseHandler = latchStreamHandlerPanic(state, baseHandler)
	streamChain := chainStreamInterceptors(h.streamInterceptors, state)
	streamCtx := contextWithMetadata(withStreamSession(session.ctx, session), ctx)
	if streamChain == nil {
		events = baseHandler(streamCtx, req)
	} else {
		events = streamChain(streamCtx, req, baseHandler)
	}
	if events == nil {
		return NewError(CodeInternal, "stream interceptor returned a nil iterator")
	}

	ctx.panicRecovery.own(func() {
		ctx.writer.Header().Set("Content-Type", "text/event-stream")
		ctx.writer.Header().Set("Cache-Control", "no-cache")
		ctx.writer.Header().Set("Connection", "keep-alive")
		ctx.writer.Header().Set("X-Accel-Buffering", "no")
	})

	writeTimeout := h.effectiveWriteTimeout(ctx)
	if err := state.writeFrame(ctx, writeTimeout, nil); err != nil {
		if !state.started {
			return NewError(CodeInternal, "streaming write deadlines or flushing not supported")
		}
		state.transportFailed = true
		return &streamTransportError{err: err}
	}

	err := h.streamEvents(ctx, executionCtx, session, state, events, writeTimeout)
	var transportErr *streamTransportError
	if errors.As(err, &transportErr) {
		state.transportFailed = true
	}
	return err
}

func (h *streamHandler[Req, Res]) streamEvents(ctx *rpcContext, executionCtx context.Context, session *streamSession, state *streamResponseState, events iter.Seq2[any, error], writeTimeout time.Duration) error {
	type eventItem struct {
		event any
		ack   chan error
	}
	eventCh := make(chan eventItem)
	producerDone := make(chan error, 1) // one-shot result; streamEvents owns cancellation and waits before returning

	go func() {
		var result error
		defer func() {
			if rec := recover(); rec != nil {
				state.handlerPanic = rec
				result = NewError(CodeInternal, "internal server error")
			}
			producerDone <- result
			close(eventCh)
		}()

		for event, err := range events {
			if err != nil {
				result = err
				return
			}
			ack := make(chan error, 1)
			select {
			case eventCh <- eventItem{event: event, ack: ack}:
			case <-session.ctx.Done():
				result = session.terminalCause()
				return
			}

			select {
			case ackErr := <-ack:
				if ackErr != nil {
					result = ackErr
					return
				}
			case <-session.ctx.Done():
				result = session.terminalCause()
				return
			}
		}
	}()

	var (
		producerFinished bool
		producerResult   error
	)
	waitForProducer := func() error {
		if !producerFinished {
			producerResult = <-producerDone
			producerFinished = true
		}
		return producerResult
	}
	defer func() {
		// Cancel the producer on every early return, including transport panics,
		// and wait for its cleanup before releasing the request.
		session.fail(ErrStreamClosed)
		waitForProducer()
	}()

	var heartbeat <-chan time.Time
	heartbeatInterval := h.effectiveHeartbeat(ctx)
	if heartbeatInterval > 0 {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		heartbeat = ticker.C
	}

	for {
		select {
		case <-executionCtx.Done():
			executionErr := executionCtx.Err()
			session.fail(executionErr)
			if ctx.request.Context().Err() != nil {
				return session.terminalCause()
			}
			return executionErr

		case <-heartbeat:
			if err := state.writeFrame(ctx, writeTimeout, []byte(": heartbeat\n\n")); err != nil {
				session.fail(err)
				return &streamTransportError{err: err}
			}

		case item, ok := <-eventCh:
			if !ok {
				result := waitForProducer()
				return result
			}

			frame, marshalErr := marshalSSEEventFrame(item.event)
			if marshalErr != nil {
				state.serializationErr = marshalErr
				cause := session.fail(marshalErr)
				item.ack <- cause
				return marshalErr
			}

			if writeErr := state.writeFrame(ctx, writeTimeout, frame); writeErr != nil {
				cause := session.fail(writeErr)
				item.ack <- cause
				return &streamTransportError{err: writeErr}
			}
			item.ack <- nil
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

func validateSSEEventID(id string) error {
	if strings.ContainsAny(id, "\r\n\x00") {
		return fmt.Errorf("%w: ID contains CR, LF, or NUL", ErrInvalidStreamEventID)
	}
	return nil
}

func marshalSSEEventFrame(event any) ([]byte, error) {
	var (
		eventID    string
		hasEventID bool
	)
	if evt, ok := event.(sseEvent); ok {
		eventID = evt.id
		hasEventID = true
		event = evt.event
	}
	if err := validateSSEEventID(eventID); err != nil {
		return nil, err
	}

	data, err := json.Marshal(response{Result: event})
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}

	var frame bytes.Buffer
	if hasEventID {
		fmt.Fprintf(&frame, "id: %s\n", eventID)
	}
	fmt.Fprintf(&frame, "data: %s\n\n", data)
	return frame.Bytes(), nil
}

func marshalSSEErrorFrame(ctx *rpcContext, err error) []byte {
	prepared := prepareErrorResponse(ctx.errorTransformer, ctx.maskInternalErrors, err)
	if prepared.usedFallback {
		h := slog.Default()
		if ctx.logger != nil {
			h = ctx.logger
		}
		h.Error("error policy failed; using internal SSE fallback",
			slog.String("endpoint", ctx.EndpointID()),
			slog.Any("error", prepared.fallbackErr))
	}
	data := prepared.data
	data = bytes.TrimSuffix(data, []byte{'\n'})
	frame := make([]byte, 0, len(data)+8)
	frame = append(frame, "data: "...)
	frame = append(frame, data...)
	frame = append(frame, '\n', '\n')
	return frame
}

func (h *streamHandler[Req, Res]) logStreamFailure(ctx *rpcContext, message string, err error) {
	logger := ctx.logger
	if logger == nil {
		logger = slog.Default()
	}
	if isClientDisconnect(err) {
		logger.Debug(message,
			slog.String("endpoint", ctx.EndpointID()))
		return
	}
	logger.Error(message,
		slog.String("endpoint", ctx.EndpointID()),
		slog.Any("error", err))
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

func latchStreamHandlerPanic(state *streamResponseState, handler StreamHandlerFunc) StreamHandlerFunc {
	return func(ctx context.Context, req any) iter.Seq2[any, error] {
		defer func() {
			if rec := recover(); rec != nil {
				state.handlerPanic = rec
				panic(rec)
			}
		}()

		events := handler(ctx, req)
		if events == nil {
			return nil
		}
		return func(yield func(any, error) bool) {
			defer func() {
				if rec := recover(); rec != nil {
					state.handlerPanic = rec
					panic(rec)
				}
			}()
			events(yield)
		}
	}
}

// chainStreamInterceptors combines multiple stream interceptors into one and
// latches panics at each boundary before an outer interceptor can recover them.
func chainStreamInterceptors(interceptors []StreamInterceptor, state *streamResponseState) StreamInterceptor {
	if len(interceptors) == 0 {
		return nil
	}
	return func(ctx Context, req any, handler StreamHandlerFunc) iter.Seq2[any, error] {
		var chain StreamHandlerFunc = handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			current := interceptors[i]
			next := chain
			chain = latchStreamHandlerPanic(state, func(c context.Context, r any) iter.Seq2[any, error] {
				return current(contextWithMetadata(c, ctx), r, next)
			})
		}
		return chain(ctx, req)
	}
}
