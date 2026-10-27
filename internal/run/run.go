// Package run starts bwrap and the sandbox.
package run

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/dlawregiets/kandor/internal/sandbox"
)

// MarkInheritedFDsCloseOnExec sets FD_CLOEXEC on every descriptor above
// stderr.  Go opens its own descriptors close-on-exec, but descriptors
// inherited from kandor's parent are not, and they would otherwise leak into
// the sandbox.
func MarkInheritedFDsCloseOnExec() error {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
	return nil
}

func probeUserns(f sandbox.Features) string {
	if f.UnshareUserTry {
		return "--unshare-user-try"
	}
	return "--unshare-user"
}

func quiet(args ...string) error {
	cmd := exec.Command("bwrap", args...)
	return cmd.Run()
}

// Preflight checks that bwrap can create a user namespace, so the user gets
// a useful error instead of a cryptic failure after all the planning.
func Preflight(f sandbox.Features) error {
	if quiet("--ro-bind", "/", "/", probeUserns(f), "--", "/bin/true") != nil {
		return preflightError()
	}
	return nil
}

func preflightError() error {
	return errors.New(`bubblewrap cannot create a user namespace.

This is usually one of:
  * unprivileged user namespaces are disabled
      sudo sysctl -w kernel.unprivileged_userns_clone=1
  * AppArmor restricts unprivileged user namespaces (Ubuntu 23.10+)
      see /etc/apparmor.d/unprivileged_userns
  * bwrap is running inside a container that blocks namespace creation
  * bwrap needs to be installed setuid root (check your distribution's
    bubblewrap package)

Run 'bwrap --ro-bind / / --unshare-user-try -- /bin/true' to reproduce.`)
}

// DisableUsernsWorks reports whether this kernel accepts --disable-userns.
func DisableUsernsWorks(f sandbox.Features) bool {
	return f.DisableUserns && quiet("--ro-bind", "/", "/", "--unshare-user", "--disable-userns", "--", "/bin/true") == nil
}

// The signals kandor handles.  SIGINT and SIGQUIT come from the terminal
// and also reach the sandbox; SIGTERM and SIGHUP are forwarded to it.
var handled = []os.Signal{syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP}

// Guard catches signals from before the sandbox starts until kandor has
// cleaned up, so an interrupted run still removes its temporary files and
// audits the git directory.  Create it before any temporary files exist and
// Stop it after cleanup.
type Guard struct {
	ch chan os.Signal
}

// NewGuard starts catching the handled signals.  A signal that was ignored
// when kandor started (nohup, a background job) stays ignored: it is neither
// caught nor forwarded to the sandbox.
func NewGuard() *Guard {
	g := &Guard{ch: make(chan os.Signal, 8)}
	var catch []os.Signal
	for _, sig := range handled {
		if !signal.Ignored(sig) {
			catch = append(catch, sig)
		}
	}
	if len(catch) > 0 {
		signal.Notify(g.ch, catch...)
	}
	return g
}

// Stop restores the default signal handling.
func (g *Guard) Stop() { signal.Stop(g.ch) }

// Pending returns a signal that arrived while no sandbox was running.
func (g *Guard) Pending() (syscall.Signal, bool) {
	select {
	case sig := <-g.ch:
		return sig.(syscall.Signal), true
	default:
		return 0, false
	}
}

// Child runs argv with a temporary Guard; see Guard.Child.
func Child(argv []string) int {
	g := NewGuard()
	defer g.Stop()
	return g.Child(argv)
}

// Child runs argv with kandor's standard streams and returns its exit
// status (128 + n if it was killed by signal n).  While it runs, SIGINT and
// SIGQUIT are left to the child (the terminal delivers them to it), and
// SIGTERM and SIGHUP are forwarded to it, so kandor outlives the sandbox and
// can clean up.
func (g *Guard) Child(argv []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "kandor: error: %v\n", err)
		return 127
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-g.ch:
				if sig == syscall.SIGTERM || sig == syscall.SIGHUP {
					cmd.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	if err == nil {
		return 0
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return exit.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "kandor: error: %v\n", err)
	return 1
}
