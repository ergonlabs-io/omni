//go:build unix

package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// forwardedSignals are delivered to the child rather than left to Go's
// default disposition, which would either kill omni (orphaning the child) or
// be ignored -- neither of which is what a terminal-wrapping process should
// do. This covers signals sent directly to the omni process (a process
// supervisor, `kill <pid>`, the controlling terminal's session-leader
// SIGHUP on hangup). It is distinct from -- and in addition to -- the
// keystroke-driven case: once the parent terminal is in raw mode, Ctrl-C and
// friends are no longer turned into signals by the *outer* tty at all; they
// pass through as raw bytes over the PTY and it is the child's own PTY slave
// (in whatever line-discipline mode the child chooses) that turns them into
// signals for the child. See internal-docs/05-constraints.md §4.
var forwardedSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// Run launches spec.Path inside a PTY and proxies the caller's terminal to
// it until the child exits or ctx is canceled. See the package doc for the
// stdout-discipline and non-interactive-stdin behavior.
//
// On ctx cancellation, Run sends SIGTERM to the child, waits up to
// ShutdownGrace, and escalates to SIGKILL if the child has not exited.
func Run(ctx context.Context, spec Spec) (Result, error) {
	stdin := spec.stdinOrDefault()
	stdout := spec.stdoutOrDefault()
	diag := spec.diagnosticsOrDefault()
	logf := func(format string, args ...any) {
		fmt.Fprintf(diag, "omni: launcher: "+format+"\n", args...)
	}

	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Env = spec.Env
	cmd.Dir = spec.Dir
	// Prefer a graceful SIGTERM over the default hard Kill() on context
	// cancellation, with WaitDelay bounding how long we wait before Go
	// escalates to SIGKILL for us. Both must be set before Start (via
	// pty.Start below), since the context-watcher goroutine that reads them
	// is spun up inside Start.
	cmd.Cancel = func() error {
		logf("context canceled: sending SIGTERM to child (pid %d)", cmd.Process.Pid)
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = ShutdownGrace

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return Result{}, fmt.Errorf("launcher: start pty: %w", err)
	}
	defer ptmx.Close()

	guard := newTermGuard()
	defer guard.restore()

	stdinFile, interactive := asTerminalFile(stdin)
	if interactive {
		oldState, err := term.MakeRaw(int(stdinFile.Fd()))
		if err != nil {
			logf("failed to set raw mode, continuing without it: %v", err)
			interactive = false
		} else {
			guard.set(int(stdinFile.Fd()), oldState)
			syncSize(stdinFile, ptmx, logf) // initial size sync
		}
	}

	copyDone := make(chan struct{})
	var wg sync.WaitGroup

	// child PTY -> caller stdout. This is the direction we actually wait
	// for: draining the child's final output (including any cleanup
	// redraws) before we restore the terminal or return.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				guard.restore()
				logf("recovered from panic copying child output: %v", r)
			}
		}()
		_, cerr := io.Copy(stdout, ptmx)
		if cerr != nil && !isBenignCopyErr(cerr) {
			logf("copying child output: %v", cerr)
		}
	}()

	// caller stdin -> child PTY. Deliberately not tracked by wg: a read from
	// a real terminal has no way to be interrupted short of closing the fd
	// itself (which we don't own), so this goroutine may still be blocked
	// on Read when Run returns. That is harmless -- the whole process exits
	// shortly after the caller propagates our exit code -- and is the same
	// tradeoff every PTY-wrapping tool (ssh, tmux, script) makes.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				guard.restore()
				logf("recovered from panic copying child input: %v", r)
			}
		}()
		_, _ = io.Copy(ptmx, stdin)
	}()

	if interactive {
		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)
		defer signal.Stop(winch)
		go func() {
			for {
				select {
				case _, ok := <-winch:
					if !ok {
						return
					}
					syncSize(stdinFile, ptmx, logf)
				case <-copyDone:
					return
				}
			}
		}()
	}

	fwd := make(chan os.Signal, 8)
	signal.Notify(fwd, forwardedSignals...)
	defer signal.Stop(fwd)
	go func() {
		for {
			select {
			case sig, ok := <-fwd:
				if !ok {
					return
				}
				if cmd.Process != nil {
					_ = cmd.Process.Signal(sig)
				}
			case <-copyDone:
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	close(copyDone)
	_ = ptmx.Close() // unblocks the child-output copy goroutine if it hasn't already seen EOF/EIO
	wg.Wait()

	guard.restore()

	return exitResult(waitErr)
}

// asTerminalFile reports whether r is an *os.File backed by a real terminal.
// Only that case enables raw mode, initial size sync, and SIGWINCH
// forwarding; every other case (a non-terminal file, or any io.Reader that
// isn't a *os.File at all, e.g. in tests) takes the non-interactive path.
func asTerminalFile(r io.Reader) (*os.File, bool) {
	f, ok := r.(*os.File)
	if !ok {
		return nil, false
	}
	return f, term.IsTerminal(int(f.Fd()))
}

// syncSize copies the terminal size from "from" to "to" (a PTY master),
// logging failures rather than treating them as fatal -- a stuck size is a
// cosmetic problem, not a reason to kill the session.
func syncSize(from, to *os.File, logf func(string, ...any)) {
	if err := pty.InheritSize(from, to); err != nil {
		logf("resize: %v", err)
	}
}

// isBenignCopyErr reports whether err is an expected consequence of shutting
// the PTY down (as opposed to a real I/O problem worth surfacing).
func isBenignCopyErr(err error) bool {
	return errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO)
}

// exitResult converts the error from cmd.Wait into a Result, applying the
// 128+signal convention for signal-terminated children.
func exitResult(err error) (Result, error) {
	if err == nil {
		return Result{ExitCode: 0}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				return Result{ExitCode: 128 + int(ws.Signal())}, nil
			}
			return Result{ExitCode: ws.ExitStatus()}, nil
		}
		return Result{ExitCode: exitErr.ExitCode()}, nil
	}
	return Result{}, fmt.Errorf("launcher: wait for child: %w", err)
}

// termGuard restores a terminal's saved state exactly once, regardless of
// how many times restore is called or from how many goroutines. It is set
// up as soon as raw mode is entered and restored from: a defer in Run (the
// normal-exit and early-return-error paths), an explicit call after the
// child exits (so the terminal is back to normal before Run returns to its
// caller), and a recover in every goroutine Run spawns (so a panic in a
// background copy goroutine -- which unwinds independently of Run's own
// defer stack -- still restores the terminal before that goroutine dies).
// See internal-docs/05-constraints.md §4: leaving raw mode on is "the worst
// failure mode we can ship."
type termGuard struct {
	mu    sync.Mutex
	once  sync.Once
	fd    int
	state *term.State
}

func newTermGuard() *termGuard { return &termGuard{} }

// set records the fd/state pair to restore. Safe to call before restore is
// ever invoked; a guard that never had set called is a no-op on restore.
func (g *termGuard) set(fd int, state *term.State) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fd = fd
	g.state = state
}

// restore restores the terminal to its saved state, if any was recorded.
// Idempotent and safe to call from multiple goroutines and multiple times.
func (g *termGuard) restore() {
	g.once.Do(func() {
		g.mu.Lock()
		fd, state := g.fd, g.state
		g.mu.Unlock()
		if state != nil {
			_ = term.Restore(fd, state)
		}
	})
}
