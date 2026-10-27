package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpand(t *testing.T) {
	home := "/home/u"
	cases := map[string]string{
		"~":        "/home/u",
		"~/x":      "/home/u/x",
		"$HOME":    "/home/u",
		"$HOME/x":  "/home/u/x",
		"~user/x":  "~user/x",
		"/a/~":     "/a/~",
		"x/$HOME":  "x/$HOME",
		"/abs/dir": "/abs/dir",
	}
	for in, want := range cases {
		if got := Expand(in, home); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(target)
	got, err := Resolve(link)
	if err != nil || got != want {
		t.Fatalf("Resolve(link) = %q, %v; want %q", got, err, want)
	}
	if _, err := Resolve(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("Resolve(missing) succeeded, want error")
	}
}

func TestResolveMaybe(t *testing.T) {
	dir := t.TempDir()
	real, _ := filepath.EvalSymlinks(dir)
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	got := ResolveMaybe(filepath.Join(dir, "link", "missing", "child"), "/")
	want := filepath.Join(real, "target", "missing", "child")
	if got != want {
		t.Errorf("ResolveMaybe(link/missing/child) = %q, want %q", got, want)
	}
	if got := ResolveMaybe("rel/x", real); got != filepath.Join(real, "rel", "x") {
		t.Errorf("ResolveMaybe(relative) = %q, want it joined to cwd", got)
	}
}

func TestIsBroad(t *testing.T) {
	h := Host{Home: "/srv/users/u", RuntimeDir: "/run/user/1000", UID: 1000}
	for _, p := range []string{"/", "/srv/users/u", "/srv/users", "/run/user/1000", "/run/user", "/opt"} {
		if !h.IsBroad(p) {
			t.Errorf("IsBroad(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"/srv/users/u/go", "/usr/local/bin", "/srv/users/other"} {
		if h.IsBroad(p) {
			t.Errorf("IsBroad(%q) = true, want false", p)
		}
	}
}

func TestContains(t *testing.T) {
	if !Contains("/a/b", "/a/b") {
		t.Error("Contains(/a/b, /a/b) = false")
	}
	if !Contains("/a", "/a/b") {
		t.Error("Contains(/a, /a/b) = false")
	}
	if Contains("/a/bc", "/a/b") {
		t.Error("Contains(/a/bc, /a/b) = true")
	}
	if Contains("/a/b", "/a/bc") {
		t.Error("Contains(/a/b, /a/bc) = true")
	}
}
