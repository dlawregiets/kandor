package run

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dlawregiets/kandor/internal/sandbox"
)

func TestChildExitCode(t *testing.T) {
	if code := Child([]string{"sh", "-c", "exit 7"}, nil); code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
	if code := Child([]string{"true"}, nil); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestChildSignalStatus(t *testing.T) {
	if code := Child([]string{"sh", "-c", "kill -TERM $$"}, nil); code != 143 {
		t.Errorf("exit code = %d, want 143", code)
	}
}

// A signal ignored when kandor starts (nohup, background jobs) must stay
// ignored: neither caught nor forwarded to the sandbox.
func TestGuardRespectsIgnoredSignals(t *testing.T) {
	signal.Ignore(syscall.SIGHUP)
	defer signal.Reset(syscall.SIGHUP)
	g := NewGuard()
	defer g.Stop()
	go func() {
		time.Sleep(300 * time.Millisecond)
		syscall.Kill(os.Getpid(), syscall.SIGHUP)
	}()
	if code := g.Child([]string{"sh", "-c", "sleep 1; exit 0"}, nil); code != 0 {
		t.Errorf("exit code = %d, want 0: an ignored SIGHUP was forwarded", code)
	}
}

func TestGuardForwardsTerm(t *testing.T) {
	g := NewGuard()
	defer g.Stop()
	go func() {
		time.Sleep(300 * time.Millisecond)
		syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	if code := g.Child([]string{"sh", "-c", "sleep 5"}, nil); code != 143 {
		t.Errorf("exit code = %d, want 143", code)
	}
}

// A signal that arrives before the sandbox starts is reported, so kandor can
// clean up and exit instead of dying with its temporary files in place.
func TestGuardPendingBeforeChild(t *testing.T) {
	g := NewGuard()
	defer g.Stop()
	if _, ok := g.Pending(); ok {
		t.Fatal("Pending reported a signal before any arrived")
	}
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sig, ok := g.Pending(); ok {
			if sig != syscall.SIGTERM {
				t.Errorf("Pending = %v, want SIGTERM", sig)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("SIGTERM before the sandbox started was not reported")
}

// The child gets exactly the environment it is given, and nothing of
// kandor's own.
func TestChildEnvironment(t *testing.T) {
	t.Setenv("KANDOR_TEST_HOST_ONLY", "leak")
	env := []string{"PATH=/usr/bin:/bin", "KANDOR_TEST_VAR=given"}
	check := []string{"sh", "-c", `test "$KANDOR_TEST_VAR" = given && test -z "$KANDOR_TEST_HOST_ONLY"`}
	if code := Child(check, env); code != 0 {
		t.Errorf("child environment was not exactly %q", env)
	}
}

func TestChildMissingProgram(t *testing.T) {
	if code := Child([]string{"/nonexistent/kandor-test"}, nil); code != 127 {
		t.Errorf("exit code = %d, want 127", code)
	}
}

func TestChildDoesNotInheritFDs(t *testing.T) {
	fd, err := syscall.Open("/etc/hostname", syscall.O_RDONLY, 0)
	if err != nil {
		t.Skip(err)
	}
	defer syscall.Close(fd)
	check := []string{"sh", "-c", fmt.Sprintf("test ! -e /proc/self/fd/%d", fd)}
	if code := Child(check, nil); code == 0 {
		t.Fatalf("precondition: fd %d without O_CLOEXEC should leak before MarkInheritedFDsCloseOnExec", fd)
	}
	if err := MarkInheritedFDsCloseOnExec(); err != nil {
		t.Fatal(err)
	}
	if code := Child(check, nil); code != 0 {
		t.Errorf("fd %d reached the child", fd)
	}
}

func TestPreflightMessage(t *testing.T) {
	err := preflightError()
	for _, want := range []string{"bubblewrap cannot create a user namespace", "kernel.unprivileged_userns_clone", "to reproduce"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("preflight message is missing %q", want)
		}
	}
}

func TestPreflightRuns(t *testing.T) {
	if _, err := os.Stat("/usr/bin/bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	f := sandbox.Features{UnshareUserTry: true}
	if err := Preflight(f); err != nil {
		t.Errorf("Preflight on a working bwrap: %v", err)
	}
	DisableUsernsWorks(sandbox.Features{DisableUserns: true}) // must not panic or hang
}
