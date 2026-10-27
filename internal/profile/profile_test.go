package profile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dlawregiets/kandor/internal/config"
)

func host(t *testing.T) XDG {
	root := t.TempDir()
	return XDG{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		State: filepath.Join(root, "state"), Cache: filepath.Join(root, "cache"),
	}
}

func TestSetupIsolated(t *testing.T) {
	h := host(t)
	home := filepath.Join(t.TempDir(), "kandor")
	s := config.Settings{Isolated: true, Profile: "work", Seed: true}
	l, cleanup, err := Setup(s, h, home, t.TempDir(), 1000)
	if err != nil {
		t.Fatalf("Setup error: %v", err)
	}
	root := filepath.Join(home, "work")
	if l.ProfileRoot != root || l.Config != root+"/config" || l.Data != root+"/data" ||
		l.State != root+"/state" || l.Cache != root+"/cache" {
		t.Errorf("layout = %+v", l)
	}
	if !l.Isolated || !l.Seed || l.HostConfig != h.Config || l.HostData != h.Data || l.HostCache != h.Cache {
		t.Errorf("layout flags/host dirs = %+v", l)
	}
	for _, d := range []string{l.Config, l.Data, l.State, l.Cache} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("%s not created", d)
		}
	}
	if st, _ := os.Stat(root); st.Mode().Perm() != 0o700 {
		t.Errorf("profile root mode = %v", st.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(l.Config); err != nil {
		t.Error("cleanup removed the isolated profile")
	}
}

func TestSetupIsolatedDefaultHome(t *testing.T) {
	h := host(t)
	l, _, err := Setup(config.Settings{Isolated: true, Profile: "default"}, h, "", t.TempDir(), 1000)
	if err != nil || l.ProfileRoot != filepath.Join(h.Data, "kandor", "default") {
		t.Errorf("ProfileRoot = %q, %v", l.ProfileRoot, err)
	}
}

func TestSetupSharedUsesHostData(t *testing.T) {
	h := host(t)
	tmp := t.TempDir()
	l, cleanup, err := Setup(config.Settings{}, h, "", tmp, 1000)
	if err != nil {
		t.Fatalf("Setup error: %v", err)
	}
	if l.Data != h.Data || l.Isolated || l.ProfileRoot != "" {
		t.Errorf("layout = %+v", l)
	}
	shared := filepath.Dir(l.Config)
	if filepath.Dir(shared) != tmp || !strings.HasPrefix(filepath.Base(shared), "kandor-shared-1000.") {
		t.Errorf("shared dir = %q", shared)
	}
	if l.State != shared+"/state" || l.Cache != shared+"/cache" {
		t.Errorf("layout = %+v", l)
	}
	if pid, _ := os.ReadFile(filepath.Join(shared, "pid")); strings.TrimSpace(string(pid)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file = %q", pid)
	}
	if st, _ := os.Stat(shared); st.Mode().Perm() != 0o700 {
		t.Errorf("shared dir mode = %v", st.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(shared); err == nil {
		t.Error("cleanup did not remove the shared directory")
	}
}

func deadPID(t *testing.T) int {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func staleDir(t *testing.T, tmp, name string, pid int, age time.Duration) string {
	d := filepath.Join(tmp, name)
	os.Mkdir(d, 0o700)
	os.WriteFile(filepath.Join(d, "pid"), []byte(fmt.Sprintf("%d\n", pid)), 0o600)
	old := time.Now().Add(-age)
	os.Chtimes(d, old, old)
	return d
}

func TestSetupRemovesStaleShared(t *testing.T) {
	tmp := t.TempDir()
	dead := staleDir(t, tmp, "kandor-shared-1000.dead", deadPID(t), 2*time.Hour)
	alive := staleDir(t, tmp, "kandor-shared-1000.alive", os.Getpid(), 2*time.Hour)
	recent := staleDir(t, tmp, "kandor-shared-1000.recent", deadPID(t), time.Minute)
	other := staleDir(t, tmp, "kandor-shared-1001.dead", deadPID(t), 2*time.Hour)
	_, cleanup, err := Setup(config.Settings{}, host(t), "", tmp, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := os.Stat(dead); err == nil {
		t.Error("stale directory of a dead process was kept")
	}
	for _, d := range []string{alive, recent, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was removed", filepath.Base(d))
		}
	}
}

func TestCleanupIdempotent(t *testing.T) {
	_, cleanup, err := Setup(config.Settings{}, host(t), "", t.TempDir(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	cleanup()
}
