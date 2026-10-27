// Package profile sets up the directories that hold the agent's state for one
// run: a persistent isolated profile, or a temporary copy in shared mode.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dlawregiets/kandor/internal/agent"
	"github.com/dlawregiets/kandor/internal/config"
)

// XDG holds the host's XDG base directories.
type XDG struct {
	Config, Data, State, Cache string
}

// Setup creates the sandbox's XDG directories and returns their layout and a
// cleanup function (safe to call more than once).
//
// An isolated profile lives in kandorHome/<profile> (kandorHome defaults to
// host.Data/kandor) and persists.  Shared mode uses a temporary directory in
// tmpdir for config, state and cache, and the host's data directory for the
// session database; the temporary directory is removed by cleanup.
func Setup(s config.Settings, host XDG, kandorHome, tmpdir string, uid int) (agent.Layout, func(), error) {
	l := agent.Layout{
		HostConfig: host.Config, HostData: host.Data, HostState: host.State, HostCache: host.Cache,
		Isolated: s.Isolated, Seed: s.Seed,
	}
	if s.Isolated {
		if kandorHome == "" {
			kandorHome = filepath.Join(host.Data, "kandor")
		}
		l.ProfileRoot = filepath.Join(kandorHome, s.Profile)
		l.Config, l.Data = filepath.Join(l.ProfileRoot, "config"), filepath.Join(l.ProfileRoot, "data")
		l.State, l.Cache = filepath.Join(l.ProfileRoot, "state"), filepath.Join(l.ProfileRoot, "cache")
		for _, d := range []string{l.Config, l.Data, l.State, l.Cache} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return agent.Layout{}, nil, fmt.Errorf("cannot create the isolated profile at %s", l.ProfileRoot)
			}
		}
		os.Chmod(l.ProfileRoot, 0o700)
		return l, func() {}, nil
	}

	prefix := fmt.Sprintf("kandor-shared-%d.", uid)
	removeStale(tmpdir, prefix)
	shared, err := os.MkdirTemp(tmpdir, prefix)
	if err != nil {
		return agent.Layout{}, nil, fmt.Errorf("cannot create a temporary shared profile")
	}
	cleanup := func() { os.RemoveAll(shared) }
	if err := os.WriteFile(filepath.Join(shared, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		cleanup()
		return agent.Layout{}, nil, fmt.Errorf("cannot create a temporary shared profile")
	}
	l.Config, l.State, l.Cache = filepath.Join(shared, "config"), filepath.Join(shared, "state"), filepath.Join(shared, "cache")
	// OpenCode needs the real data directory for the shared session database.
	l.Data = host.Data
	for _, d := range []string{l.Config, l.State, l.Cache, l.Data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			cleanup()
			return agent.Layout{}, nil, fmt.Errorf("cannot create the temporary shared profile")
		}
	}
	return l, cleanup, nil
}

// removeStale removes shared directories left by runs that were killed
// before they could clean up, keeping any whose owning process is alive.
func removeStale(tmpdir, prefix string) {
	entries, err := os.ReadDir(tmpdir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		dir := filepath.Join(tmpdir, e.Name())
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(dir, "pid"))
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 &&
			syscall.Kill(pid, 0) == nil {
			continue
		}
		os.RemoveAll(dir)
	}
}
