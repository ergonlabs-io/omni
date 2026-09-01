// Package launcher runs a coding agent's child process inside a
// pseudo-terminal (PTY) and transparently proxies the user's real terminal
// to it, so that `omni claude` is indistinguishable from bare `claude`
// (project goal G4; see internal-docs/01-vision.md and
// internal-docs/05-constraints.md §4).
//
// # Two data paths
//
// The launcher owns exactly one of the two data paths described in
// internal-docs/02-architecture.md: the terminal path (PTY). It is a pure
// byte passthrough and never interprets the bytes flowing through it -- the
// PTY carries ANSI escape sequences from a redrawing full-screen UI, not
// structured data (internal-docs/05-constraints.md §8). The network path
// (the child's HTTPS calls) is a separate package's job.
//
// # stdout discipline
//
// The child owns the terminal. Run writes nothing of its own to the
// caller-supplied Stdout -- only bytes read from the child's PTY master ever
// reach it. Any diagnostics Run wants to report (raw-mode failures, signal
// forwarding, shutdown timing) go to Spec.Diagnostics, which is nil
// (discarded) unless the caller explicitly wires it to stderr behind a
// verbose flag. `omni claude 2>/dev/null` must be a byte-clean session
// (internal-docs/09-cli-design.md §3), and this package never writes to
// Spec.Stdout except by copying the child's own bytes.
//
// # Non-interactive stdin
//
// When Spec.Stdin is not a real terminal (piped input, CI, a *os.File that
// fails term.IsTerminal, or any io.Reader that isn't a *os.File at all), Run
// still allocates a PTY -- the child needs one to behave as a TUI at all --
// but skips raw-mode and SIGWINCH machinery entirely: there is no terminal
// size to track and no line discipline to bypass. The child runs attached to
// a PTY sized at whatever default creack/pty assigns, and input is copied to
// it as plain bytes. This degrades gracefully rather than failing: scripts
// and CI that pipe input into `omni claude` still get a working, if
// non-resizable, session.
//
// # Platform support
//
// The real implementation is unix-only ([creack/pty] plus
// golang.org/x/term). Windows ConPTY support is out of scope for v1
// (internal-docs/05-constraints.md §4); on unsupported platforms Run returns
// a clear error instead of failing to compile or behaving unpredictably.
//
// [creack/pty]: https://github.com/creack/pty
package launcher

import (
	"io"
	"os"
	"time"
)

// ShutdownGrace is how long Run waits, after sending SIGTERM to the child in
// response to context cancellation, before escalating to SIGKILL. See the
// "context cancellation" requirement in internal-docs/05-constraints.md §4.
const ShutdownGrace = 5 * time.Second

// DrainGrace is how long Run waits, after the child has exited, for the
// child's final output to come out of the PTY.
//
// Reaping the child does not mean its last bytes have been read: they can
// still be sitting in the PTY buffer, and closing the master discards them.
// Normally the copy finishes the moment the last slave descriptor closes, so
// this bound is never reached. It exists for the case where a grandchild
// inherited the PTY and holds it open, where a truncated tail is a better
// outcome than a session that never returns.
const DrainGrace = 2 * time.Second

// Spec describes the child process to launch and the terminal to proxy it
// through.
type Spec struct {
	// Path is the resolved executable to run (see profile.Profile.Resolve).
	Path string
	// Args are passed through to the child verbatim. omni never parses agent
	// arguments itself (internal-docs/09-cli-design.md §1) and neither does
	// Run.
	Args []string
	// Env is the full environment for the child process. It replaces,
	// rather than extends, the omni process's own environment -- callers
	// that want to inherit os.Environ() must include it themselves. See
	// profile.Profile.Env, which returns only the additions omni layers on
	// top of whatever the caller assembles.
	Env []string
	// Dir is the child's working directory. Empty means the working
	// directory of the omni process.
	Dir string

	// Stdin is the reader Run copies into the child's PTY. Defaults to
	// os.Stdin when nil. Only a *os.File backed by a real terminal enables
	// raw mode, the initial size sync, and SIGWINCH forwarding; any other
	// value (including a non-terminal *os.File) takes the non-interactive
	// path described in the package doc.
	Stdin io.Reader
	// Stdout receives everything the child writes. A PTY carries both
	// stdout and stderr for the child, exactly as a bare terminal session
	// would -- there is no separate child-stderr stream to plumb. Defaults
	// to os.Stdout when nil. Run writes nothing of its own here.
	Stdout io.Writer
	// Diagnostics receives Run's own verbose/debug output -- never the
	// child's. Nil discards it. Callers should wire this to stderr, and
	// only when a verbose flag is set (internal-docs/09-cli-design.md §3);
	// Run must never be pointed at stdout here.
	Diagnostics io.Writer
}

// Result is the outcome of a completed child process.
type Result struct {
	// ExitCode is the child's process exit code, or 128+signal if the child
	// was terminated by a signal -- the same convention a POSIX shell uses.
	// Callers should use this as omni's own exit code, so that scripts
	// wrapping `omni claude` see exactly what bare `claude` would give them
	// (internal-docs/09-cli-design.md §4).
	ExitCode int
}

// stdinOrDefault returns s.Stdin, or os.Stdin if unset.
func (s Spec) stdinOrDefault() io.Reader {
	if s.Stdin != nil {
		return s.Stdin
	}
	return os.Stdin
}

// stdoutOrDefault returns s.Stdout, or os.Stdout if unset.
func (s Spec) stdoutOrDefault() io.Writer {
	if s.Stdout != nil {
		return s.Stdout
	}
	return os.Stdout
}

// diagnosticsOrDefault returns s.Diagnostics, or io.Discard if unset.
func (s Spec) diagnosticsOrDefault() io.Writer {
	if s.Diagnostics != nil {
		return s.Diagnostics
	}
	return io.Discard
}
