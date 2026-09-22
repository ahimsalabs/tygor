package tygor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type bareSSEWriter struct{ http.ResponseWriter }

type flushOnlySSEWriter struct{ http.ResponseWriter }

func (w *flushOnlySSEWriter) FlushError() error {
	return http.NewResponseController(w.ResponseWriter).Flush()
}

type deadlineOnlySSEWriter struct{ http.ResponseWriter }

func (w *deadlineOnlySSEWriter) SetWriteDeadline(deadline time.Time) error {
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(deadline)
}

type rejectingDeadlineSSEWriter struct{ http.ResponseWriter }

func (w *rejectingDeadlineSSEWriter) FlushError() error {
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *rejectingDeadlineSSEWriter) SetWriteDeadline(time.Time) error {
	return http.ErrNotSupported
}

type panicAfterCommitSSEWriter struct {
	http.ResponseWriter
	flushes int
}

func (w *panicAfterCommitSSEWriter) FlushError() error {
	w.flushes++
	if w.flushes == 2 {
		panic("post-commit transport panic")
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

type unwrapSSEWriter struct{ http.ResponseWriter }

func (w *unwrapSSEWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type deadlineAwareWriter struct {
	header           http.Header
	deadline         time.Time
	deadlineAtWrite  time.Time
	deadlineAtFlush  time.Time
	writes           int
	flushes          int
	setDeadlineErr   error
	clearDeadlineErr error
	writeErr         error
	flushErr         error
}

type deadlineBoundaryWriter struct {
	header            http.Header
	body              strings.Builder
	statusCalls       int
	writes            int
	flushes           int
	deadlineCalls     int
	panicOnDeadline   int
	deadlineErrOnCall int
	deadlineErr       error
	panicValue        any
}

func (w *deadlineBoundaryWriter) Header() http.Header { return w.header }

func (w *deadlineBoundaryWriter) WriteHeader(int) {
	w.statusCalls++
}

func (w *deadlineBoundaryWriter) Write(data []byte) (int, error) {
	w.writes++
	return w.body.Write(data)
}

func (w *deadlineBoundaryWriter) FlushError() error {
	w.flushes++
	return nil
}

func (w *deadlineBoundaryWriter) SetWriteDeadline(time.Time) error {
	w.deadlineCalls++
	if w.deadlineCalls == w.panicOnDeadline {
		panic(w.panicValue)
	}
	if w.deadlineCalls == w.deadlineErrOnCall {
		return w.deadlineErr
	}
	return nil
}

type panicUnwrapSSEWriter struct {
	http.ResponseWriter
	panicValue  any
	unwrapCalls int
}

func (w *panicUnwrapSSEWriter) Unwrap() http.ResponseWriter {
	w.unwrapCalls++
	panic(w.panicValue)
}

type writeAndClearPanicWriter struct {
	header         http.Header
	panicAt        string
	operationPanic any
	clearPanic     any
	deadlineCalls  int
}

func (w *writeAndClearPanicWriter) Header() http.Header { return w.header }
func (w *writeAndClearPanicWriter) WriteHeader(int)     {}

func (w *writeAndClearPanicWriter) Write(data []byte) (int, error) {
	if w.panicAt == "write" {
		panic(w.operationPanic)
	}
	return len(data), nil
}

func (w *writeAndClearPanicWriter) FlushError() error {
	if w.panicAt == "flush" {
		panic(w.operationPanic)
	}
	return nil
}

func (w *writeAndClearPanicWriter) SetWriteDeadline(time.Time) error {
	w.deadlineCalls++
	if w.deadlineCalls == 2 {
		panic(w.clearPanic)
	}
	return nil
}

type headerPanicSSEWriter struct {
	header      http.Header
	panicValue  any
	headerCalls int
	writes      int
	flushes     int
}

func (w *headerPanicSSEWriter) Header() http.Header {
	w.headerCalls++
	if w.headerCalls == 1 {
		panic(w.panicValue)
	}
	return w.header
}

func (*headerPanicSSEWriter) WriteHeader(int) {}

func (w *headerPanicSSEWriter) Write(data []byte) (int, error) {
	w.writes++
	return len(data), nil
}

func (w *headerPanicSSEWriter) FlushError() error {
	w.flushes++
	return nil
}

func (*headerPanicSSEWriter) SetWriteDeadline(time.Time) error { return nil }

func (w *deadlineAwareWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *deadlineAwareWriter) WriteHeader(int) {}

func (w *deadlineAwareWriter) Write(p []byte) (int, error) {
	w.writes++
	w.deadlineAtWrite = w.deadline
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}

func (w *deadlineAwareWriter) FlushError() error {
	w.flushes++
	w.deadlineAtFlush = w.deadline
	return w.flushErr
}

func (w *deadlineAwareWriter) SetWriteDeadline(deadline time.Time) error {
	if deadline.IsZero() && w.clearDeadlineErr != nil {
		return w.clearDeadlineErr
	}
	if w.setDeadlineErr != nil {
		return w.setDeadlineErr
	}
	w.deadline = deadline
	return nil
}

func TestWriteSSEFrameBoundsWriteAndFlush(t *testing.T) {
	w := &deadlineAwareWriter{}
	if err := writeSSEFrame(w, time.Second, []byte("data: {}\n\n")); err != nil {
		t.Fatalf("writeSSEFrame() error = %v", err)
	}
	if w.writes != 1 || w.flushes != 1 {
		t.Fatalf("writes/flushes = %d/%d, want 1/1", w.writes, w.flushes)
	}
	if w.deadlineAtWrite.IsZero() {
		t.Fatal("write ran without a deadline")
	}
	if w.deadlineAtFlush.IsZero() {
		t.Fatal("flush ran without a deadline")
	}
	if !w.deadline.IsZero() {
		t.Fatal("deadline was not cleared after flush")
	}
}

func TestWriteSSEFrameBoundsHeaderFlush(t *testing.T) {
	w := &deadlineAwareWriter{}
	if err := writeSSEFrame(w, time.Second, nil); err != nil {
		t.Fatalf("writeSSEFrame() error = %v", err)
	}
	if w.writes != 0 || w.flushes != 1 {
		t.Fatalf("writes/flushes = %d/%d, want 0/1", w.writes, w.flushes)
	}
	if w.deadlineAtFlush.IsZero() {
		t.Fatal("header flush ran without a deadline")
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

func TestWriteSSEFrameReturnsFlushTimeout(t *testing.T) {
	w := &deadlineAwareWriter{flushErr: timeoutError{}}
	err := writeSSEFrame(w, time.Second, []byte("data: {}\n\n"))
	if !errors.Is(err, ErrWriteTimeout) {
		t.Fatalf("writeSSEFrame() error = %v, want ErrWriteTimeout", err)
	}
	if w.deadlineAtFlush.IsZero() {
		t.Fatal("failing flush ran without a deadline")
	}
	if !w.deadline.IsZero() {
		t.Fatal("deadline was not cleared after failing flush")
	}
}

func TestWriteSSEFrameClearsDeadlineAfterWriteFailure(t *testing.T) {
	writeErr := errors.New("write failed")
	w := &deadlineAwareWriter{writeErr: writeErr}
	err := writeSSEFrame(w, time.Second, []byte("data: {}\n\n"))
	if !errors.Is(err, writeErr) {
		t.Fatalf("writeSSEFrame() error = %v, want write failure", err)
	}
	if w.deadlineAtWrite.IsZero() {
		t.Fatal("failing write ran without a deadline")
	}
	if !w.deadline.IsZero() {
		t.Fatal("deadline was not cleared after failing write")
	}
	if w.flushes != 0 {
		t.Fatalf("flushes = %d, want 0 after write failure", w.flushes)
	}
}

func TestWriteSSEFramePreservesFlushAndDeadlineClearErrors(t *testing.T) {
	flushErr := errors.New("flush failed")
	clearErr := errors.New("clear failed")
	w := &deadlineAwareWriter{flushErr: flushErr, clearDeadlineErr: clearErr}
	err := writeSSEFrame(w, time.Second, []byte("data: {}\n\n"))
	if !errors.Is(err, flushErr) {
		t.Fatalf("writeSSEFrame() error = %v, want flush failure", err)
	}
	if !errors.Is(err, clearErr) {
		t.Fatalf("writeSSEFrame() error = %v, want clear failure", err)
	}
	if w.flushes != 1 {
		t.Fatalf("flushes = %d, want 1", w.flushes)
	}
}

func TestWriteSSEFrameRejectsUnsupportedDeadline(t *testing.T) {
	w := &deadlineAwareWriter{setDeadlineErr: http.ErrNotSupported}
	err := writeSSEFrame(w, time.Second, []byte("data: {}\n\n"))
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("writeSSEFrame() error = %v, want http.ErrNotSupported", err)
	}
	if w.writes != 0 || w.flushes != 0 {
		t.Fatalf("writes/flushes = %d/%d, want 0/0", w.writes, w.flushes)
	}
}

func TestWriteSSEFramePreservesOriginalPanicWithoutDeadlineCleanup(t *testing.T) {
	for _, panicAt := range []string{"write", "flush"} {
		t.Run(panicAt, func(t *testing.T) {
			operationPanic := &struct{ name string }{panicAt}
			clearPanic := &struct{ name string }{"clear"}
			w := &writeAndClearPanicWriter{
				header:         make(http.Header),
				panicAt:        panicAt,
				operationPanic: operationPanic,
				clearPanic:     clearPanic,
			}

			recovered := func() (recovered any) {
				defer func() { recovered = recover() }()
				_ = writeSSEFrame(w, time.Second, []byte("data: {}\n\n"))
				return nil
			}()

			if recovered != operationPanic {
				t.Fatalf("recovered = %v, want original %s panic", recovered, panicAt)
			}
			if w.deadlineCalls != 1 {
				t.Fatalf("deadline calls = %d, want no cleanup during panic unwinding", w.deadlineCalls)
			}
		})
	}
}

func TestSSEDeadlinePanicRemainsTransportOwned(t *testing.T) {
	for _, endpoint := range []string{"stream", "livevalue"} {
		for _, panicOnCall := range []int{1, 3} {
			t.Run(fmt.Sprintf("%s/call_%d", endpoint, panicOnCall), func(t *testing.T) {
				panicValue := &struct{}{}
				var transformerCalls atomic.Int32
				var producerStarts atomic.Int32
				app := NewApp().
					WithStreamWriteTimeout(time.Second).
					WithStreamHeartbeat(0).
					WithErrorTransformer(func(error) *Error {
						transformerCalls.Add(1)
						return NewError(CodeInternal, "transformed")
					})

				var liveValue *LiveValue[int]
				path := "/Feed/Subscribe"
				if endpoint == "stream" {
					app.Service("Feed").Register("Subscribe", Stream(func(context.Context, Empty, StreamWriter[int]) error {
						producerStarts.Add(1)
						return nil
					}))
				} else {
					liveValue = mustNewLiveValue(t, 1)
					app.Service("State").Register("Get", liveValue.Handler())
					path = "/State/Get"
				}

				writer := &deadlineBoundaryWriter{
					header:          make(http.Header),
					panicOnDeadline: panicOnCall,
					panicValue:      panicValue,
				}
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")

				recovered := serveAndRecover(app.Handler(), writer, req)

				if recovered != panicValue {
					t.Fatalf("recovered = %v, want original deadline panic", recovered)
				}
				if got := transformerCalls.Load(); got != 0 {
					t.Fatalf("transformer calls = %d, want 0", got)
				}
				if writer.deadlineCalls != panicOnCall || writer.statusCalls != 0 || writer.writes != 0 || writer.flushes != 0 || writer.body.Len() != 0 {
					t.Fatalf("transport attempts = deadline:%d status:%d write:%d flush:%d body:%q, want %d/0/0/0/empty", writer.deadlineCalls, writer.statusCalls, writer.writes, writer.flushes, writer.body.String(), panicOnCall)
				}
				if producerStarts.Load() != 0 {
					t.Fatalf("producer starts = %d, want 0", producerStarts.Load())
				}
				if liveValue != nil {
					liveValue.mu.RLock()
					nextSubID := liveValue.nextSubID
					subscribers := len(liveValue.subscribers)
					liveValue.mu.RUnlock()
					wantNextSubID := int64(0)
					if panicOnCall == 3 {
						wantNextSubID = 1
					}
					if nextSubID != wantNextSubID || subscribers != 0 {
						t.Fatalf("livevalue subscription state = next:%d active:%d, want %d/0", nextSubID, subscribers, wantNextSubID)
					}
				}
			})
		}
	}
}

func TestSSEReturnedFirstFrameDeadlineErrorUsesUnarySetupError(t *testing.T) {
	for _, endpoint := range []string{"stream", "livevalue"} {
		t.Run(endpoint, func(t *testing.T) {
			var producerStarts atomic.Int32
			app := NewApp().WithStreamWriteTimeout(time.Second).WithStreamHeartbeat(0)
			var liveValue *LiveValue[int]
			path := "/Feed/Subscribe"
			if endpoint == "stream" {
				app.Service("Feed").Register("Subscribe", Stream(func(context.Context, Empty, StreamWriter[int]) error {
					producerStarts.Add(1)
					return nil
				}))
			} else {
				liveValue = mustNewLiveValue(t, 1)
				app.Service("State").Register("Get", liveValue.Handler())
				path = "/State/Get"
			}

			writer := &deadlineBoundaryWriter{
				header:            make(http.Header),
				deadlineErrOnCall: 3,
				deadlineErr:       errors.New("deadline setup failed"),
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")

			app.Handler().ServeHTTP(writer, req)

			if writer.deadlineCalls != 3 || writer.statusCalls != 1 || writer.writes != 1 || writer.flushes != 0 {
				t.Fatalf("transport attempts = deadline:%d status:%d write:%d flush:%d, want 3/1/1/0", writer.deadlineCalls, writer.statusCalls, writer.writes, writer.flushes)
			}
			if writer.header.Get("Content-Type") != "application/json" || !strings.Contains(writer.body.String(), `"code":"internal"`) {
				t.Fatalf("response = %q %q, want unary internal JSON", writer.header.Get("Content-Type"), writer.body.String())
			}
			if producerStarts.Load() != 0 {
				t.Fatalf("producer starts = %d, want 0", producerStarts.Load())
			}
			if liveValue != nil {
				liveValue.mu.RLock()
				nextSubID := liveValue.nextSubID
				subscribers := len(liveValue.subscribers)
				liveValue.mu.RUnlock()
				if nextSubID != 1 || subscribers != 0 {
					t.Fatalf("livevalue subscription state = next:%d active:%d, want 1/0", nextSubID, subscribers)
				}
			}
		})
	}
}

func TestSSEUnwrapPanicRemainsTransportOwned(t *testing.T) {
	for _, endpoint := range []string{"stream", "livevalue"} {
		t.Run(endpoint, func(t *testing.T) {
			panicValue := &struct{}{}
			var transformerCalls atomic.Int32
			var producerStarts atomic.Int32
			app := NewApp().WithErrorTransformer(func(error) *Error {
				transformerCalls.Add(1)
				return NewError(CodeInternal, "transformed")
			})
			var liveValue *LiveValue[int]
			path := "/Feed/Subscribe"
			if endpoint == "stream" {
				app.Service("Feed").Register("Subscribe", Stream(func(context.Context, Empty, StreamWriter[int]) error {
					producerStarts.Add(1)
					return nil
				}))
			} else {
				liveValue = mustNewLiveValue(t, 1)
				app.Service("State").Register("Get", liveValue.Handler())
				path = "/State/Get"
			}
			writer := &panicUnwrapSSEWriter{
				ResponseWriter: httptest.NewRecorder(),
				panicValue:     panicValue,
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")

			recovered := serveAndRecover(app.Handler(), writer, req)

			if recovered != panicValue {
				t.Fatalf("recovered = %v, want original unwrap panic", recovered)
			}
			if writer.unwrapCalls != 1 || transformerCalls.Load() != 0 || producerStarts.Load() != 0 {
				t.Fatalf("unwrap/transformer/producer calls = %d/%d/%d, want 1/0/0", writer.unwrapCalls, transformerCalls.Load(), producerStarts.Load())
			}
			if liveValue != nil {
				liveValue.mu.RLock()
				nextSubID := liveValue.nextSubID
				subscribers := len(liveValue.subscribers)
				liveValue.mu.RUnlock()
				if nextSubID != 0 || subscribers != 0 {
					t.Fatalf("livevalue registered before unwrap completed: next:%d active:%d", nextSubID, subscribers)
				}
			}
		})
	}
}

func TestStreamUnaryRetryDoesNotReplayHeaderPanic(t *testing.T) {
	panicValue := &struct{}{}
	var setupCalls atomic.Int32
	var producerStarts atomic.Int32
	app := NewApp().WithStreamWriteTimeout(time.Second).WithStreamHeartbeat(0)
	app.WithUnaryInterceptor(func(ctx Context, req any, next HandlerFunc) (any, error) {
		setupCalls.Add(1)
		_, _ = next(ctx, req)
		setupCalls.Add(1)
		_, _ = next(ctx, req)
		return nil, nil
	})
	app.Service("Feed").Register("Subscribe", Stream(func(context.Context, Empty, StreamWriter[int]) error {
		producerStarts.Add(1)
		return nil
	}))
	writer := &headerPanicSSEWriter{header: make(http.Header), panicValue: panicValue}
	req := httptest.NewRequest(http.MethodPost, "/Feed/Subscribe", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	recovered := serveAndRecover(app.Handler(), writer, req)

	if recovered != panicValue {
		t.Fatalf("recovered panic = %v, want original panic", recovered)
	}
	if setupCalls.Load() != 2 || producerStarts.Load() != 0 {
		t.Fatalf("setup/producer calls = %d/%d, want 2/0", setupCalls.Load(), producerStarts.Load())
	}
	if writer.headerCalls != 1 || writer.writes != 0 || writer.flushes != 0 {
		t.Fatalf("transport attempts = header:%d write:%d flush:%d, want 1/0/0", writer.headerCalls, writer.writes, writer.flushes)
	}
}

func TestSSECapabilityPreflightThroughAppHandler(t *testing.T) {
	wrappers := []struct {
		name    string
		timeout time.Duration
		wrap    func(http.ResponseWriter) http.ResponseWriter
	}{
		{
			name:    "timeout zero still requires flushing",
			timeout: 0,
			wrap:    func(w http.ResponseWriter) http.ResponseWriter { return &bareSSEWriter{ResponseWriter: w} },
		},
		{
			name:    "deadline support without flushing",
			timeout: time.Second,
			wrap:    func(w http.ResponseWriter) http.ResponseWriter { return &deadlineOnlySSEWriter{ResponseWriter: w} },
		},
		{
			name:    "deadline method rejects setup",
			timeout: time.Second,
			wrap:    func(w http.ResponseWriter) http.ResponseWriter { return &rejectingDeadlineSSEWriter{ResponseWriter: w} },
		},
	}

	for _, wrapper := range wrappers {
		for _, endpoint := range []string{"stream", "livevalue"} {
			t.Run(endpoint+"/"+wrapper.name, func(t *testing.T) {
				app := NewApp().WithStreamWriteTimeout(wrapper.timeout).WithStreamHeartbeat(0)
				app.WithMiddleware(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						next.ServeHTTP(wrapper.wrap(w), r)
					})
				})

				var producerStarts atomic.Int32
				var liveValue *LiveValue[int]
				path := "/Feed/Subscribe"
				if endpoint == "stream" {
					app.Service("Feed").Register("Subscribe", Stream(func(_ context.Context, _ Empty, stream StreamWriter[int]) error {
						producerStarts.Add(1)
						return stream.Send(1)
					}))
				} else {
					liveValue = mustNewLiveValue(t, 1)
					app.Service("State").Register("Get", liveValue.Handler())
					path = "/State/Get"
				}

				status, contentType, body := postSSETestServer(t, app, path, nil)
				if status != http.StatusInternalServerError || contentType != "application/json" {
					t.Fatalf("response = %d %q body=%q, want unary 500 application/json", status, contentType, body)
				}
				if strings.Contains(body, "data: ") || !strings.Contains(body, `"code":"internal"`) {
					t.Fatalf("body = %q, want structured unary setup error", body)
				}
				if producerStarts.Load() != 0 {
					t.Fatal("stream producer started before capability preflight")
				}
				if liveValue != nil {
					liveValue.mu.RLock()
					nextSubID := liveValue.nextSubID
					subscribers := len(liveValue.subscribers)
					liveValue.mu.RUnlock()
					if nextSubID != 0 || subscribers != 0 {
						t.Fatalf("livevalue registered before capability preflight: nextSubID=%d subscribers=%d", nextSubID, subscribers)
					}
				}
			})
		}
	}
}

func TestSSECapabilityPreflightAcceptsSupportedWriters(t *testing.T) {
	wrappers := []struct {
		name               string
		timeout            time.Duration
		handlerTimeoutZero bool
		wrap               func(http.ResponseWriter) http.ResponseWriter
	}{
		{
			name:    "unwrap chain",
			timeout: time.Second,
			wrap:    func(w http.ResponseWriter) http.ResponseWriter { return &unwrapSSEWriter{ResponseWriter: w} },
		},
		{
			name:    "flush only without deadline support",
			timeout: time.Second,
			wrap:    func(w http.ResponseWriter) http.ResponseWriter { return &flushOnlySSEWriter{ResponseWriter: w} },
		},
	}

	for _, wrapper := range wrappers {
		for _, endpoint := range []string{"stream", "livevalue"} {
			t.Run(endpoint+"/"+wrapper.name, func(t *testing.T) {
				app := NewApp().WithStreamWriteTimeout(wrapper.timeout).WithStreamHeartbeat(0)
				app.WithMiddleware(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						next.ServeHTTP(wrapper.wrap(w), r)
					})
				})

				var producerStarts atomic.Int32
				var closeStream func()
				path := "/Feed/Subscribe"
				if endpoint == "stream" {
					handler := Stream(func(_ context.Context, _ Empty, stream StreamWriter[int]) error {
						producerStarts.Add(1)
						return stream.Send(1)
					})
					if wrapper.handlerTimeoutZero {
						handler.WithWriteTimeout(0)
					}
					app.Service("Feed").Register("Subscribe", handler)
				} else {
					liveValue := mustNewLiveValue(t, 1)
					handler := liveValue.Handler()
					if wrapper.handlerTimeoutZero {
						handler.WithWriteTimeout(0)
					}
					app.Service("State").Register("Get", handler)
					path = "/State/Get"
					closeStream = liveValue.Close
				}

				status, contentType, body := postSSETestServer(t, app, path, closeStream)
				if status != http.StatusOK || contentType != "text/event-stream" || !strings.Contains(body, `"result":1`) {
					t.Fatalf("response = %d %q body=%q, want successful SSE", status, contentType, body)
				}
				if endpoint == "stream" && producerStarts.Load() != 1 {
					t.Fatalf("stream producer starts = %d, want 1", producerStarts.Load())
				}
			})
		}
	}
}

func TestLiveValuePostCommitPanicDoesNotFallBackToUnaryJSON(t *testing.T) {
	app := NewApp().WithStreamWriteTimeout(0).WithStreamHeartbeat(0)
	app.WithMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&panicAfterCommitSSEWriter{ResponseWriter: w}, r)
		})
	})
	liveValue := mustNewLiveValue(t, 1)
	app.Service("State").Register("Get", liveValue.Handler())
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/State/Get", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(recorder, req)
	}()

	if recovered != "post-commit transport panic" {
		t.Fatalf("recovered = %v, want original transport panic", recovered)
	}
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("response = %d %q, want committed SSE", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	if body := recorder.Body.String(); strings.Contains(body, `"error"`) || !strings.Contains(body, `"result":1`) {
		t.Fatalf("body = %q, want only the committed SSE value", body)
	}
}

func postSSETestServer(t *testing.T, app *App, path string, afterHeaders func()) (int, string, string) {
	t.Helper()
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(server.URL+path, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST error = %v", err)
	}
	if afterHeaders != nil {
		afterHeaders()
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}
