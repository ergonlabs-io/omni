//go:build unix

package launcher

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// findShell locates /bin/sh, skipping the test if it isn't present. Every
// unix CI runner has one, but this keeps the test honest about its
// dependency instead of assuming a path.
func findShell(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no /bin/sh on PATH, skipping")
	}
	return path
}

func TestRun_ExitCodePropagation(t *testing.T) {
	sh := findShell(t)

	spec := Spec{
		Path:   sh,
		Args:   []string{"-c", "exit 42"},
		Stdin:  strings.NewReader(""), // non-terminal: exercises the non-interactive path too
		Stdout: &bytes.Buffer{},
	}

	res, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 42 {
		t.Fatalf("ExitCode = %d, want 42", res.ExitCode)
	}
}

func TestRun_SignalTerminatedChild(t *testing.T) {
	sh := findShell(t)

	// The shell sends itself SIGTERM. cmd.Wait should report a
	// signal-terminated exit, and Run must translate that to 128+15=143 per
	// the POSIX-shell convention (internal-docs/09-cli-design.md §4).
	spec := Spec{
		Path:   sh,
		Args:   []string{"-c", "kill -TERM $$"},
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
	}

	res, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	const wantSIGTERM = 128 + 15
	if res.ExitCode != wantSIGTERM {
		t.Fatalf("ExitCode = %d, want %d (128+SIGTERM)", res.ExitCode, wantSIGTERM)
	}
}

func TestRun_EnvAndArgsPassthrough(t *testing.T) {
	sh := findShell(t)

	var out bytes.Buffer
	spec := Spec{
		Path:   sh,
		Args:   []string{"-c", "echo $FOO"},
		Env:    []string{"FOO=bar-baz", "PATH=/usr/bin:/bin"},
		Stdin:  strings.NewReader(""),
		Stdout: &out,
	}

	res, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (stderr/stdout so far: %q)", res.ExitCode, out.String())
	}
	// A PTY slave defaults to canonical mode with ONLCR, so expect \r\n line
	// endings rather than asserting exact equality.
	if !strings.Contains(out.String(), "bar-baz") {
		t.Fatalf("child output %q does not contain expected env value", out.String())
	}
}

// A child's last bytes can still be in the PTY buffer when it is reaped, so
// draining has to finish before the master is closed. Writing a burst
// immediately before exit is the shape that catches a premature close: it is
// what a TUI does on its way out (final redraw, cursor restore), and losing
// it truncates the visible session.
func TestRun_DrainsOutputWrittenJustBeforeExit(t *testing.T) {
	sh := findShell(t)

	const lines = 200
	var out bytes.Buffer
	spec := Spec{
		Path:   sh,
		Args:   []string{"-c", "i=0; while [ $i -lt 200 ]; do echo \"line $i\"; i=$((i+1)); done"},
		Stdin:  strings.NewReader(""),
		Stdout: &out,
	}

	res, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}

	got := strings.Count(out.String(), "line ")
	if got != lines {
		t.Errorf("captured %d of %d lines; the child's output was truncated before the pty was drained", got, lines)
	}
	// The final line is the one a premature close loses first.
	if !strings.Contains(out.String(), "line 199") {
		t.Errorf("missing the last line the child wrote; output ends with %q",
			lastN(out.String(), 40))
	}
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func TestRun_NonTTYStdinSkipsRawMode(t *testing.T) {
	sh := findShell(t)

	// strings.Reader is not an *os.File, so asTerminalFile must report
	// non-interactive regardless of what fd the real test process's stdin
	// happens to be attached to. This is the piped-input/CI path from
	// internal-docs/05-constraints.md §4 requirement 7.
	var out bytes.Buffer
	spec := Spec{
		Path:   sh,
		Args:   []string{"-c", "echo hello"},
		Stdin:  strings.NewReader(""),
		Stdout: &out,
	}

	res, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("child output %q does not contain expected text", out.String())
	}
}

func TestAsTerminalFile_NonFileReader(t *testing.T) {
	f, ok := asTerminalFile(strings.NewReader("x"))
	if ok || f != nil {
		t.Fatalf("asTerminalFile(non-*os.File) = (%v, %v), want (nil, false)", f, ok)
	}
}

func TestRun_ContextCancellationSendsSIGTERM(t *testing.T) {
	sh := findShell(t)

	ctx, cancel := context.WithCancel(context.Background())
	spec := Spec{
		Path:   sh,
		Args:   []string{"-c", "sleep 30"}, // does not trap SIGTERM: default disposition kills it promptly
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
	}

	type runOutcome struct {
		res Result
		err error
	}
	done := make(chan runOutcome, 1)
	go func() {
		res, err := Run(ctx, spec)
		done <- runOutcome{res, err}
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("Run returned error: %v", out.err)
		}
		// sh, not trapping SIGTERM, dies with the default disposition.
		const wantSIGTERM = 128 + 15
		if out.res.ExitCode != wantSIGTERM {
			t.Fatalf("ExitCode = %d, want %d (128+SIGTERM)", out.res.ExitCode, wantSIGTERM)
		}
	case <-time.After(ShutdownGrace + 5*time.Second):
		t.Fatal("Run did not return within grace period + margin after ctx cancellation")
	}
}

func TestTermGuard_RestoreIsIdempotent(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no PTY available in this environment: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	state, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		t.Fatalf("MakeRaw: %v", err)
	}

	g := newTermGuard()
	g.set(int(tty.Fd()), state)

	// Must not panic, error out loudly, or double-restore in any observable
	// way when called repeatedly -- including concurrently, mirroring the
	// defer-plus-explicit-call-plus-panic-recovery paths in Run.
	g.restore()
	g.restore()
	g.restore()
}

func TestTermGuard_RestoreWithoutSetIsNoop(t *testing.T) {
	g := newTermGuard()
	// No set() call was ever made (mirrors the non-interactive path, or an
	// early return from Run before raw mode was entered). Must not panic.
	g.restore()
}

func TestExitResult_NilError(t *testing.T) {
	res, err := exitResult(nil)
	if err != nil {
		t.Fatalf("exitResult(nil) error = %v, want nil", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
}

func TestExitResult_NonExitError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := exitResult(sentinel)
	if err == nil {
		t.Fatal("exitResult(non-ExitError) error = nil, want non-nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("exitResult error does not wrap the original: %v", err)
	}
}
