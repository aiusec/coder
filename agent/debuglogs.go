package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"cdr.dev/slog/v3"
)

const (
	debugLogsActiveLimitBytes      = 10 * 1024 * 1024
	debugLogsWithRotatedLimitBytes = 100 * 1024 * 1024
)

var coderAgentRotatedLogPattern = regexp.MustCompile(`^coder-agent-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}\.log$`)

type agentLogFile struct {
	path    string
	name    string
	modTime time.Time
}

func (a *agent) HandleHTTPDebugLogs(w http.ResponseWriter, r *http.Request) {
	after, ok, err := parseDebugLogsAfter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !ok {
		a.writeActiveDebugLog(w, r)
		return
	}

	if err := disableWriteDeadline(w); err != nil {
		a.logger.Warn(r.Context(), "disable debug log write deadline", slog.Error(err))
	}
	files, err := agentDebugLogFiles(r.Context(), a.logger, a.logDir, after)
	if err != nil {
		a.logger.Error(r.Context(), "find agent log files", slog.Error(err), slog.F("log_dir", a.logDir))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "could not find log files: %s", err)
		return
	}
	w.WriteHeader(http.StatusOK)
	remaining := int64(debugLogsWithRotatedLimitBytes)
	wroteAny := false
	for _, file := range files {
		if remaining <= 0 {
			break
		}
		f, err := os.Open(file.path)
		if err != nil {
			a.logger.Warn(r.Context(), "open agent log file", slog.Error(err), slog.F("path", file.path))
			continue
		}
		copyErr := func() error {
			defer f.Close()
			if wroteAny {
				if err := writeLimitedDebugLogString(w, "\n", &remaining); err != nil || remaining <= 0 {
					return err
				}
			}
			if err := writeLimitedDebugLogString(w, agentLogBoundary(file), &remaining); err != nil || remaining <= 0 {
				return err
			}
			wroteAny = true
			n, err := io.Copy(w, io.LimitReader(f, remaining))
			remaining -= n
			return err
		}()
		if copyErr != nil {
			a.logger.Error(r.Context(), "read agent log file", slog.Error(copyErr), slog.F("path", file.path))
			return
		}
	}
	if remaining <= 0 {
		a.logger.Warn(r.Context(), "agent debug logs response truncated", slog.F("limit_bytes", debugLogsWithRotatedLimitBytes))
	}
}

func parseDebugLogsAfter(r *http.Request) (time.Time, bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("after"))
	if raw == "" {
		return time.Time{}, false, nil
	}
	after, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false, xerrors.Errorf("after must be an RFC3339 timestamp: %w", err)
	}
	return after, true, nil
}

func (a *agent) writeActiveDebugLog(w http.ResponseWriter, r *http.Request) {
	logPath := filepath.Join(a.logDir, "coder-agent.log")
	f, err := os.Open(logPath)
	if err != nil {
		a.logger.Error(r.Context(), "open agent log file", slog.Error(err), slog.F("path", logPath))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "could not open log file: %s", err)
		return
	}
	defer f.Close()

	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, io.LimitReader(f, debugLogsActiveLimitBytes))
	if err != nil {
		a.logger.Error(r.Context(), "read agent log file", slog.Error(err))
		return
	}
}

func agentDebugLogFiles(ctx context.Context, logger slog.Logger, logDir string, after time.Time) ([]agentLogFile, error) {
	activePath := filepath.Join(logDir, "coder-agent.log")
	activeInfo, err := os.Stat(activePath)
	if err != nil {
		return nil, xerrors.Errorf("stat active log: %w", err)
	}
	files := []agentLogFile{{
		path:    activePath,
		name:    filepath.Base(activePath),
		modTime: activeInfo.ModTime(),
	}}

	matches, err := filepath.Glob(filepath.Join(logDir, "coder-agent-*.log"))
	if err != nil {
		return nil, xerrors.Errorf("glob rotated logs: %w", err)
	}
	rotated := make([]agentLogFile, 0, len(matches))
	for _, match := range matches {
		base := filepath.Base(match)
		if !coderAgentRotatedLogPattern.MatchString(base) {
			continue
		}
		info, err := os.Stat(match)
		if err != nil {
			logger.Warn(ctx, "stat rotated agent log file", slog.Error(err), slog.F("path", match))
			continue
		}
		if !info.Mode().IsRegular() || info.ModTime().Before(after) {
			continue
		}
		rotated = append(rotated, agentLogFile{
			path:    match,
			name:    base,
			modTime: info.ModTime(),
		})
	}
	slices.SortFunc(rotated, func(a, b agentLogFile) int {
		return b.modTime.Compare(a.modTime)
	})
	files = append(files, rotated...)
	return files, nil
}

func agentLogBoundary(file agentLogFile) string {
	return fmt.Sprintf("=== %s (mtime %s) ===\n", file.name, file.modTime.UTC().Format(time.RFC3339))
}

func writeLimitedDebugLogString(w io.Writer, s string, remaining *int64) error {
	if int64(len(s)) > *remaining {
		*remaining = 0
		return nil
	}
	n, err := io.WriteString(w, s)
	*remaining -= int64(n)
	return err
}

func disableWriteDeadline(w http.ResponseWriter) error {
	return http.NewResponseController(w).SetWriteDeadline(time.Time{})
}
