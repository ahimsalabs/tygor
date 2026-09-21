//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"tygor.dev/tygor"
)

type streamEvent struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

type panicJSONEvent struct{}

func (panicJSONEvent) MarshalJSON() ([]byte, error) {
	panic("private marshaler panic")
}

type errorJSONEvent struct {
	ID  int
	Err error
}

func (event errorJSONEvent) MarshalJSON() ([]byte, error) {
	if event.Err != nil {
		return nil, event.Err
	}
	return json.Marshal(struct {
		ID int `json:"id"`
	}{ID: event.ID})
}

func TestStreamMarshalPanicAfterCommitRejectsRealClientReads(t *testing.T) {
	app := tygor.NewApp(tygor.WithMaskInternalErrors(), tygor.WithStreamWriteTimeout(0))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ tygor.Empty, stream tygor.StreamWriter[panicJSONEvent]) error {
		return stream.Send(panicJSONEvent{})
	})
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)

	runStreamClient(t, "stream-terminal-error.ts", server.URL, "internal", "internal server error", false, "", false)
}

func TestStreamInterceptorCannotRecoverProducerPanicFromRealClient(t *testing.T) {
	recoveringInterceptor := func(ctx tygor.Context, req any, handler tygor.StreamHandlerFunc) iter.Seq2[any, error] {
		events := handler(ctx, req)
		return func(yield func(any, error) bool) {
			defer func() { _ = recover() }()
			events(yield)
		}
	}
	app := tygor.NewApp(tygor.WithMaskInternalErrors(), tygor.WithStreamWriteTimeout(0))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ tygor.Empty, stream tygor.StreamWriter[streamEvent]) error {
		if err := stream.Send(streamEvent{ID: 1, Message: "before panic"}); err != nil {
			return err
		}
		panic("private producer panic")
	}, tygor.WithStreamInterceptors(recoveringInterceptor))
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)

	runStreamClient(t, "stream-terminal-error.ts", server.URL, "internal", "internal server error", true, "before panic", false)
}

func TestStreamUnaryInterceptorSetupRecoveryDoesNotAffectStreamLifetime(t *testing.T) {
	recovered := make(chan struct{}, 1)
	outer := func(ctx tygor.Context, req any, next tygor.HandlerFunc) (response any, err error) {
		defer func() {
			if recover() != nil {
				select {
				case recovered <- struct{}{}:
				default:
				}
				response = nil
				err = nil
			}
		}()
		return next(ctx, req)
	}
	inner := func(ctx tygor.Context, req any, next tygor.HandlerFunc) (any, error) {
		defer panic("private unary cleanup panic")
		return next(ctx, req)
	}
	app := tygor.NewApp(
		tygor.WithMaskInternalErrors(),
		tygor.WithStreamWriteTimeout(0),
		tygor.WithUnaryInterceptors(outer, inner),
	)
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ tygor.Empty, stream tygor.StreamWriter[streamEvent]) error {
		return stream.Send(streamEvent{ID: 1, Message: "before panic"})
	})
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)

	runStreamClient(t, "stream-terminal-error.ts", server.URL, "", "", true, "before panic", true)
	select {
	case <-recovered:
	default:
		t.Fatal("outer unary interceptor did not recover the cleanup panic")
	}
}

func TestStreamUnaryInterceptorDoesNotObserveCommittedMarshalErrorFromRealClient(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		wantCode    string
		wantMessage string
	}{
		{name: "ordinary", err: errors.New("private marshal failure"), wantCode: "internal", wantMessage: "internal server error"},
		{name: "wrapped stream closed", err: fmt.Errorf("private marshal failure: %w", tygor.ErrStreamClosed), wantCode: "canceled", wantMessage: "stream closed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed := make(chan struct{}, 1)
			interceptor := func(ctx tygor.Context, req any, next tygor.HandlerFunc) (any, error) {
				_, err := next(ctx, req)
				if err != nil {
					observed <- struct{}{}
				}
				return nil, nil
			}
			app := tygor.NewApp(
				tygor.WithMaskInternalErrors(),
				tygor.WithStreamWriteTimeout(0),
				tygor.WithUnaryInterceptors(interceptor),
			)
			app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ tygor.Empty, stream tygor.StreamWriter[errorJSONEvent]) error {
				if err := stream.Send(errorJSONEvent{ID: 1}); err != nil {
					return err
				}
				return stream.Send(errorJSONEvent{Err: test.err})
			})
			server := httptest.NewServer(app.Handler())
			t.Cleanup(server.Close)

			runStreamClient(t, "stream-terminal-error.ts", server.URL, test.wantCode, test.wantMessage, true, "", false)
			select {
			case <-observed:
				t.Fatal("setup-only unary interceptor observed a stream-lifetime marshal error")
			default:
			}
		})
	}
}

type partialEventWriter struct {
	http.ResponseWriter
	failed bool
}

func (w *partialEventWriter) Write(p []byte) (int, error) {
	if !w.failed && len(p) > 0 {
		w.failed = true
		n, _ := w.ResponseWriter.Write(p[:1])
		return n, errors.New("partial event write")
	}
	return w.ResponseWriter.Write(p)
}

func (w *partialEventWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestStreamPartialEventWriteDoesNotCompleteRealClientRead(t *testing.T) {
	app := tygor.NewApp(tygor.WithStreamWriteTimeout(0))
	app.Service("Feed").Stream("Subscribe", func(_ context.Context, _ tygor.Empty, stream tygor.StreamWriter[streamEvent]) error {
		return stream.Send(streamEvent{ID: 1})
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		app.Handler().ServeHTTP(&partialEventWriter{ResponseWriter: w}, req)
	}))
	t.Cleanup(server.Close)

	runBunFixture(t, "stream-partial-write.ts", "TYGOR_BASE_URL="+server.URL)
}

func runStreamClient(t *testing.T, fixture, baseURL, code, message string, expectFirst bool, firstMessage string, expectCompletion bool) {
	t.Helper()
	runBunFixture(t, fixture,
		"TYGOR_BASE_URL="+baseURL,
		"TYGOR_ERROR_CODE="+code,
		"TYGOR_ERROR_MESSAGE="+message,
		fmt.Sprintf("TYGOR_EXPECT_FIRST=%t", expectFirst),
		"TYGOR_EXPECT_FIRST_MESSAGE="+firstMessage,
		fmt.Sprintf("TYGOR_EXPECT_COMPLETION=%t", expectCompletion),
	)
}

func runBunFixture(t *testing.T, fixture string, env ...string) {
	t.Helper()
	if _, err := exec.LookPath("bun"); err != nil {
		t.Fatal("bun is required for e2e tests")
	}
	path, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bun", "run", path)
	cmd.Env = append(os.Environ(), env...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bun run %s failed: %v\n%s", fixture, err, output)
	}
}
