package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dlawregiets/kandor/internal/config"
)

// generic is a config-defined agent: no bespoke behavior, just the declared
// identity, subcommands, exclusions and state paths.
type generic struct {
	spec Spec
}

func (g generic) Name() string       { return g.spec.Name }
func (g generic) Executable() string { return g.spec.Binary }

func (g generic) IsSubcommand(arg string) bool {
	return slices.Contains(g.spec.Subcommands, arg)
}

func (g generic) ExcludeEnv(name string) bool {
	for _, e := range g.spec.EnvExclude {
		if e != "" && (name == e || (strings.HasSuffix(e, "_") && strings.HasPrefix(name, e))) {
			return true
		}
	}
	return false
}

// Prepare provides the persistent caches a generic agent asks for.  Host
// state itself arrives through Materialize, which app applies from the
// merged spec after Prepare.
func (g generic) Prepare(l Layout, warn func(string)) (State, error) {
	if l.HostCache == "" || (!g.spec.NodeCompileCache && !g.spec.PersistentScratch) {
		return State{}, nil
	}
	root := filepath.Join(l.HostCache, "kandor", g.spec.Name)
	// dir creates one kandor-owned persistent directory, pruning entries
	// older than keep when keep is positive.
	dir := func(name string, keep time.Duration) string {
		path := filepath.Join(root, name)
		if l.DryRun {
			return path
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			warn(fmt.Sprintf("cannot create %s: %v", path, err))
			return ""
		}
		if keep > 0 {
			prune(path, keep)
		}
		return path
	}
	var st State
	if g.spec.NodeCompileCache {
		if path := dir("node-compile-cache", 0); path != "" {
			st.Binds = append(st.Binds, Bind{Src: path, Dest: path, Writable: true})
			st.Env = append(st.Env, EnvVar{Name: "NODE_COMPILE_CACHE", Value: path})
		}
	}
	if g.spec.PersistentScratch {
		if path := dir("tmp", scratchKeep); path != "" {
			st.Binds = append(st.Binds, Bind{Src: path, Dest: path, Writable: true})
			st.Env = append(st.Env, EnvVar{Name: "TMPDIR", Value: path})
		}
	}
	return st, nil
}

// scratchKeep is how long a scratch entry may sit untouched before kandor
// removes it.  The scratch directory is the sandbox's TMPDIR, so on-disk
// caches kept there (a module loader's transpiled sources, for one) save
// real time between runs, but leftovers from killed runs must not pile
// up forever.
const scratchKeep = 14 * 24 * time.Hour

// prune removes top-level scratch entries older than keep.  An entry
// counts as touched when anything inside it is newer than the cutoff:
// a directory's own mod-time does not change when a file inside it is
// rewritten, so checking only the top level would delete entries a
// concurrent run is still using.
func prune(dir string, keep time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-keep)
	for _, e := range entries {
		if !stale(filepath.Join(dir, e.Name()), cutoff) {
			continue
		}
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// stale reports whether nothing at or below path has been touched since
// cutoff.  Symlinks are not followed, so an entry cannot keep itself (or
// be deleted) through a link to a file elsewhere.
func stale(path string, cutoff time.Time) bool {
	stale := true
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(cutoff) {
			stale = false
			return filepath.SkipAll
		}
		return nil
	})
	return stale
}

// Command passes arguments through unchanged.
func (g generic) Command(bin string, args []string) ([]string, error) {
	return append([]string{bin}, args...), nil
}

// For returns the runtime agent: OpenCode keeps its bespoke behavior,
// everything else is generic over the merged section.
func For(name string, cfg config.AgentSection) Agent {
	if name == OpenCode.Name() {
		return OpenCode
	}
	return generic{spec: Resolve(name, cfg)}
}

// Subcommands returns the known subcommands for name from built-in defaults
// plus the user file's top-level section: enough to tell a project directory
// apart from a subcommand while parsing, before layers merge.
func Subcommands(name string, cfg config.AgentSection) []string {
	s, _ := builtin(name)
	return appendUnique(slices.Clone(s.Subcommands), cfg.Subcommands)
}

// Known lists the selectable agents: built-ins plus every configured
// [agents.<name>] section in the given files, sorted.
func Known(files ...map[string]config.AgentSection) []string {
	var seen []string
	for _, s := range builtinSpecs {
		seen = append(seen, s.Name)
	}
	for _, sections := range files {
		for name := range sections {
			if !slices.Contains(seen, name) {
				seen = append(seen, name)
			}
		}
	}
	slices.Sort(seen)
	return seen
}

// ProvisionalIsSubcommand matches any built-in subcommand.  It backs the
// pre-parse before the agent is resolved; the full parse uses the resolved
// agent's list instead.
func ProvisionalIsSubcommand(arg string) bool {
	for _, s := range builtinSpecs {
		if slices.Contains(s.Subcommands, arg) {
			return true
		}
	}
	return false
}

// IsSubcommandFor reports whether arg is a subcommand of the named agent.
func IsSubcommandFor(name string, cfg config.AgentSection) func(string) bool {
	subs := Subcommands(name, cfg)
	return func(arg string) bool {
		return slices.Contains(subs, arg)
	}
}
