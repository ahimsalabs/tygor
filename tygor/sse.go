package tygor

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type sseFlushError interface {
	FlushError() error
}

type sseWriteDeadline interface {
	SetWriteDeadline(time.Time) error
}

type responseWriterUnwrapper interface {
	Unwrap() http.ResponseWriter
}

// preflightSSE verifies capabilities before handlers mutate SSE headers or
// start producers/subscriptions. Flushing is mandatory. Write deadlines are
// probed when the writer supports them, but wrappers such as httptest.ResponseRecorder
// remain valid SSE writers and run without a framework-enforced write timeout.
func preflightSSE(w http.ResponseWriter, timeout time.Duration) error {
	if !supportsSSEFlush(w) {
		return fmt.Errorf("SSE flushing: %w", http.ErrNotSupported)
	}
	if timeout <= 0 {
		return nil
	}
	if !supportsSSEWriteDeadline(w) {
		return nil
	}

	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set SSE write deadline during preflight: %w", err)
	}
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear SSE write deadline during preflight: %w", err)
	}
	return nil
}

func supportsSSEFlush(w http.ResponseWriter) bool {
	for range 64 {
		switch current := w.(type) {
		case sseFlushError:
			return true
		case http.Flusher:
			return true
		case responseWriterUnwrapper:
			w = current.Unwrap()
			if w == nil {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func supportsSSEWriteDeadline(w http.ResponseWriter) bool {
	for range 64 {
		switch current := w.(type) {
		case sseWriteDeadline:
			return true
		case responseWriterUnwrapper:
			w = current.Unwrap()
			if w == nil {
				return false
			}
		default:
			return false
		}
	}
	return false
}

// writeSSEFrame performs one synchronous SSE transport operation. A positive
// timeout must be enforceable by the ResponseWriter; unsupported deadlines fail
// closed rather than silently creating an unbounded write.
func writeSSEFrame(w http.ResponseWriter, timeout time.Duration, frame []byte) (err error) {
	return writeSSEFrameWithCommit(w, timeout, frame, nil)
}

// writeSSEFrameWithCommit calls beforeCommit immediately before the operation
// can first commit response headers. It lets stream handlers distinguish a
// pre-I/O capability failure from a failure after SSE commitment.
func writeSSEFrameWithCommit(w http.ResponseWriter, timeout time.Duration, frame []byte, beforeCommit func()) (err error) {
	rc := http.NewResponseController(w)
	deadlineSupported := timeout > 0 && supportsSSEWriteDeadline(w)
	if deadlineSupported {
		if deadlineErr := rc.SetWriteDeadline(time.Now().Add(timeout)); deadlineErr != nil {
			return fmt.Errorf("set SSE write deadline: %w", deadlineErr)
		}
	}

	if beforeCommit != nil {
		beforeCommit()
	}
	if len(frame) > 0 {
		n, writeErr := w.Write(frame)
		if writeErr != nil {
			err = classifySSEWriteError("write SSE frame", writeErr)
		} else if n != len(frame) {
			err = fmt.Errorf("write SSE frame: %w", io.ErrShortWrite)
		}
	}

	if err == nil {
		if flushErr := rc.Flush(); flushErr != nil {
			err = classifySSEWriteError("flush SSE frame", flushErr)
		}
	}
	if deadlineSupported {
		if clearErr := rc.SetWriteDeadline(time.Time{}); clearErr != nil {
			clearErr = fmt.Errorf("clear SSE write deadline: %w", clearErr)
			if err == nil {
				err = clearErr
			} else {
				err = errors.Join(err, clearErr)
			}
		}
	}
	return err
}

func classifySSEWriteError(operation string, err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%s: %w: %w", operation, ErrWriteTimeout, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
