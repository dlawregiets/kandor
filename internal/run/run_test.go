package run

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/dlawregiets/kandor/internal/sandbox"
)

func TestChildExitCode(t *testing.T) {
	if code := Child([]string{"sh", "-c", "exit 7"}); code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
	if code := Child([]string{"true"}); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestChildSignalStatus(t *testing.T) {
	if code := Child([]string{"sh", "-c", "kill -TERM $$"}); code != 143 {
		t.Errorf("exit code = %d, want 143", code)
	}
}

func TestChildMissingProgram(t *testing.T) {
	if code := Child([]string{"/nonexistent/kandor-test"}); code != 127 {
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
	if code := Child(check); code == 0 {
		t.Fatalf("precondition: fd %d without O_CLOEXEC should leak before MarkInheritedFDsCloseOnExec", fd)
	}
	if err := MarkInheritedFDsCloseOnExec(); err != nil {
		t.Fatal(err)
	}
	if code := Child(check); code != 0 {
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
