package httpapi

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testLogger returns a logger writing into buf, so tests can assert on output.
// This is why logging.New returns a logger instead of setting a global.
func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestRequestIDIsSetAndReadable(t *testing.T) {
	var seen string

	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	// httptest gives us a fake request and a recorder for the response — no
	// network, no port, no server.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	header := rec.Header().Get("X-Request-ID")
	if header == "" {
		t.Fatal("X-Request-ID response header is empty")
	}
	if seen == "" {
		t.Fatal("request ID was not readable from the context")
	}
	if seen != header {
		t.Errorf("context ID %q != header ID %q", seen, header)
	}
}

func TestRecoverTurnsPanicInto500(t *testing.T) {
	var buf bytes.Buffer

	boom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went very wrong")
	})

	h := Recover(testLogger(&buf))(boom)

	rec := httptest.NewRecorder()
	// If Recover did not work, this call would panic and fail the test.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	body, _ := io.ReadAll(rec.Body)
	// The client must not see the panic text — it leaks internals.
	if strings.Contains(string(body), "something went very wrong") {
		t.Errorf("panic message leaked to the client: %s", body)
	}
	// But we must still have logged it.
	if !strings.Contains(buf.String(), "something went very wrong") {
		t.Error("panic message was not logged")
	}
}

// The reason Recover sits INSIDE AccessLog: if it were outside, AccessLog's
// deferred log line would run while the panic was still unwinding, before the
// 500 was written, and would report 200.
func TestAccessLogRecordsTheRecoveredStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf)

	boom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	})

	h := chain(boom, RequestID, AccessLog(logger), Recover(logger))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	out := buf.String()
	if !strings.Contains(out, "status=500") {
		t.Errorf("access log should report status=500, got:\n%s", out)
	}
	if !strings.Contains(out, "request_id=") {
		t.Errorf("access log should include the request ID, got:\n%s", out)
	}
}

func TestAccessLogDefaultsTo200(t *testing.T) {
	var buf bytes.Buffer

	// A handler that writes a body without ever calling WriteHeader.
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})

	h := AccessLog(testLogger(&buf))(ok)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), "status=200") {
		t.Errorf("implicit 200 not recorded, got:\n%s", buf.String())
	}
}
