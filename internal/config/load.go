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
	Explicit bool   // TOML came from KANDOR_CONFIG
}

// Locate finds the configuration file: $KANDOR_CONFIG, else
// $XDG_CONFIG_HOME/kandor/config.toml, else ~/.config/kandor/config.toml.
func Locate(getenv func(string) string) Locations {
	dir := getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(getenv("HOME"), ".config")
	}
	dir = filepath.Join(dir, "kandor")
	loc := Locations{TOML: filepath.Join(dir, "config.toml")}
	if p := getenv("KANDOR_CONFIG"); p != "" {
		loc.TOML = p
		loc.Explicit = true
	}
	return loc
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
	if err := validateEnvNames(f.Env.Pass); err != nil {
		return err
	}
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
		a := f.Agents[name]
		if err := validateEnvNames(a.Env.Pass); err != nil {
			return err
		}
		if err := validateEnvSet(a.Env.Set); err != nil {
			return err
		}
	}
	sectionNames := make([]string, 0, len(f.Sections))
	for name := range f.Sections {
		sectionNames = append(sectionNames, name)
	}
	sort.Strings(sectionNames)
	for _, name := range sectionNames {
		sec := f.Sections[name]
		if err := validateEnvNames(sec.Env.Pass); err != nil {
			return fmt.Errorf("sections.%s: %w", name, err)
		}
		if err := validateEnvSet(sec.Env.Set); err != nil {
			return fmt.Errorf("sections.%s: %w", name, err)
		}
		agentNames := make([]string, 0, len(sec.Agents))
		for agentName := range sec.Agents {
			agentNames = append(agentNames, agentName)
		}
		sort.Strings(agentNames)
		for _, agentName := range agentNames {
			if !validAgentName(agentName) {
				return fmt.Errorf("sections.%s: invalid agent name %q (use lowercase letters, digits, '-' and '_'; not \"exec\" or \"default\")", name, agentName)
			}
			a := sec.Agents[agentName]
			if err := validateEnvNames(a.Env.Pass); err != nil {
				return fmt.Errorf("sections.%s.agents.%s: %w", name, agentName, err)
			}
			if err := validateEnvSet(a.Env.Set); err != nil {
				return fmt.Errorf("sections.%s.agents.%s: %w", name, agentName, err)
			}
		}
	}
	return nil
}

func validateEnvNames(names []string) error {
	for _, name := range names {
		if !envName.MatchString(name) {
			return fmt.Errorf("invalid environment variable name: %s", name)
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
