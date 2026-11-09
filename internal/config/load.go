package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/BurntSushi/toml"
)

// Locations are the configuration file paths for this environment.
type Locations struct {
	TOML     string // the TOML file to load
	Legacy   string // the old line-based file at the standard location
	Explicit bool   // TOML came from KANDOR_CONFIG
}

// Locate finds the configuration files: $KANDOR_CONFIG, else
// $XDG_CONFIG_HOME/kandor/config.toml, else ~/.config/kandor/config.toml.
func Locate(getenv func(string) string) Locations {
	dir := getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(getenv("HOME"), ".config")
	}
	dir = filepath.Join(dir, "kandor")
	loc := Locations{TOML: filepath.Join(dir, "config.toml"), Legacy: filepath.Join(dir, "config")}
	if p := getenv("KANDOR_CONFIG"); p != "" {
		loc.TOML = p
		loc.Explicit = true
	}
	return loc
}

// LegacyError reports a configuration file in the old line-based format.
type LegacyError struct{ Path string }

func (e *LegacyError) Error() string {
	return fmt.Sprintf("%s uses the old configuration format; run 'kandor --migrate-config'", e.Path)
}

// CheckLegacy reports an old-format file at the standard location when the
// TOML file does not exist yet, so its settings are not silently ignored.
// With KANDOR_CONFIG set, only that file matters.
func CheckLegacy(loc Locations) error {
	if loc.Explicit || exists(loc.TOML) || !exists(loc.Legacy) {
		return nil
	}
	return &LegacyError{Path: loc.Legacy}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// agentName restricts [agents.<name>] sections to lowercase names; "exec"
// is the generic-command mode and "default" would confuse default_agent.
var agentName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func validAgentName(name string) bool {
	if name == "exec" || name == "default" {
		return false
	}
	return agentName.MatchString(name)
}

// Load reads the TOML file at path.  A missing file yields an empty File.
// Unknown keys, wrong types and unknown agents are errors.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		if IsLegacy(data) {
			return nil, &LegacyError{Path: path}
		}
		if pe, ok := errors.AsType[toml.ParseError](err); ok {
			return nil, fmt.Errorf("%s: line %d: %s", path, pe.Position.Line, pe.Message)
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("%s: unknown setting %q", path, keys[0])
	}
	if err := validate(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

func validate(f *File) error {
	if err := validateEnvSet(f.Env.Set); err != nil {
		return err
	}
	if f.DefaultAgent != "" && !validAgentName(f.DefaultAgent) {
		return fmt.Errorf("invalid default_agent %q (use a lowercase agent name)", f.DefaultAgent)
	}
	names := make([]string, 0, len(f.Agents))
	for name := range f.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !validAgentName(name) {
			return fmt.Errorf("invalid agent name %q (use lowercase letters, digits, '-' and '_'; not \"exec\" or \"default\")", name)
		}
		if err := validateEnvSet(f.Agents[name].Env.Set); err != nil {
			return err
		}
	}
	return nil
}

func validateEnvSet(set map[string]string) error {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !envName.MatchString(name) {
			return fmt.Errorf("invalid environment variable name: %s", name)
		}
	}
	return nil
}
