package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dlawregiets/kandor/internal/config"
)

var allFeatures = Features{true, true, true, true, true, true, true}

func namespaces(s config.Settings, f Features, disableWorks, newSession bool) []string {
	p := NewPlan(f)
	AddNamespaces(p, s, f, disableWorks, newSession, "/proj")
	return p.Args()
}

func TestNamespacesDefaults(t *testing.T) {
	args := namespaces(config.Settings{Network: true}, allFeatures, true, false)
	want := []string{"--unshare-user", "--disable-userns", "--unshare-pid", "--unshare-ipc", "--unshare-uts",
		"--unshare-cgroup-try", "--hostname", "kandor", "--die-with-parent", "--cap-drop", "ALL",
		"--chdir", "/proj", "--clearenv"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %q\nwant  %q", args, want)
	}
}

func TestNamespacesNoNet(t *testing.T) {
	if args := namespaces(config.Settings{Network: false}, allFeatures, true, false); !slices.Contains(args, "--unshare-net") {
		t.Errorf("no --unshare-net: %q", args)
	}
	if args := namespaces(config.Settings{Network: true}, allFeatures, true, false); slices.Contains(args, "--unshare-net") {
		t.Errorf("--unshare-net with network: %q", args)
	}
}

func TestNamespacesDisableUserns(t *testing.T) {
	args := namespaces(config.Settings{AllowUserns: true, Network: true}, allFeatures, true, false)
	if slices.Contains(args, "--disable-userns") || !slices.Contains(args, "--unshare-user-try") {
		t.Errorf("--allow-userns: %q", args)
	}
	args = namespaces(config.Settings{Network: true}, allFeatures, false, false)
	if slices.Contains(args, "--disable-userns") || !slices.Contains(args, "--unshare-user-try") {
		t.Errorf("unsupported --disable-userns: %q", args)
	}
	args = namespaces(config.Settings{Network: true}, Features{}, false, true)
	if !slices.Contains(args, "--unshare-user") || slices.Contains(args, "--new-session") ||
		slices.Contains(args, "--cap-drop") || slices.Contains(args, "--die-with-parent") {
		t.Errorf("old bwrap: %q", args)
	}
}

func TestNamespacesNewSession(t *testing.T) {
	if args := namespaces(config.Settings{Network: true}, allFeatures, true, true); !slices.Contains(args, "--new-session") {
		t.Errorf("no --new-session: %q", args)
	}
}

func TestNewSessionDefault(t *testing.T) {
	dir := t.TempDir()
	zero, one := filepath.Join(dir, "zero"), filepath.Join(dir, "one")
	os.WriteFile(zero, []byte("0\n"), 0o644)
	os.WriteFile(one, []byte("1\n"), 0o644)
	cases := []struct {
		mode         config.NewSessionMode
		path         string
		on, explicit bool
	}{
		{config.SessionAuto, zero, false, false},
		{config.SessionAuto, one, true, false},
		{config.SessionAuto, filepath.Join(dir, "missing"), true, false},
		{config.SessionOff, one, false, true},
		{config.SessionOn, zero, true, true},
	}
	for _, c := range cases {
		on, explicit := NewSessionDefault(c.mode, c.path)
		if on != c.on || explicit != c.explicit {
			t.Errorf("NewSessionDefault(%v, %s) = %v, %v; want %v, %v", c.mode, filepath.Base(c.path), on, explicit, c.on, c.explicit)
		}
	}
}
