package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog/v3"
)

func TestHandleHTTPDebugLogsWithAfterCapsResponse(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	activePath := filepath.Join(logDir, "coder-agent.log")
	f, err := os.Create(activePath)
	require.NoError(t, err)
	_, err = f.WriteString("active log\n")
	require.NoError(t, err)
	require.NoError(t, f.Truncate(debugLogsWithRotatedLimitBytes+1))
	require.NoError(t, f.Close())

	a := &agent{
		logger: slog.Make().Leveled(slog.LevelDebug),
		logDir: logDir,
	}
	req := httptest.NewRequest(http.MethodGet, "/debug/logs?after="+time.Now().Add(-time.Minute).Format(time.RFC3339Nano), nil)
	res := &countingResponseWriter{header: http.Header{}}

	a.HandleHTTPDebugLogs(res, req)

	require.Equal(t, http.StatusOK, res.status)
	require.Equal(t, int64(debugLogsWithRotatedLimitBytes), res.bytes)
	require.Contains(t, string(res.prefix), "coder-agent.log")
}

type countingResponseWriter struct {
	header http.Header
	status int
	bytes  int64
	prefix []byte
}

func (w *countingResponseWriter) Header() http.Header {
	return w.header
}

func (w *countingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	if len(w.prefix) < 1024 {
		remaining := 1024 - len(w.prefix)
		w.prefix = append(w.prefix, p[:min(len(p), remaining)]...)
	}
	w.bytes += int64(len(p))
	return len(p), nil
}
