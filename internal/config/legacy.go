package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// legacyDirectives are the directives of the old line-based format.
var legacyDirectives = map[string]func(f *File, rest string) error{
	"ro":              func(f *File, r string) error { f.Mounts.ReadOnly = append(f.Mounts.ReadOnly, r); return nil },
	"rw":              func(f *File, r string) error { f.Mounts.ReadWrite = append(f.Mounts.ReadWrite, r); return nil },
	"env":             legacyEnv,
	"net":             flag(func(f *File) **bool { return &f.Sandbox.Network }, true),
	"no-net":          flag(func(f *File) **bool { return &f.Sandbox.Network }, false),
	"isolated":        flag(func(f *File) **bool { return &f.Profile.Isolated }, true),
	"shared":          flag(func(f *File) **bool { return &f.Profile.Isolated }, false),
	"no-seed":         flag(func(f *File) **bool { return &f.Profile.Seed }, false),
	"ssh":             flag(func(f *File) **bool { return &f.Credentials.SSH }, true),
	"ssh-agent":       flag(func(f *File) **bool { return &f.Credentials.SSHAgent }, true),
	"git-credentials": flag(func(f *File) **bool { return &f.Credentials.GitCredentials }, true),
	"netrc":           flag(func(f *File) **bool { return &f.Credentials.Netrc }, true),
	"npmrc":           flag(func(f *File) **bool { return &f.Credentials.Npmrc }, true),
	"no-gitconfig":    flag(func(f *File) **bool { return &f.Git.MountConfig }, false),
	"protect-git":     flag(func(f *File) **bool { return &f.Git.Protect }, true),
	"no-protect-git":  flag(func(f *File) **bool { return &f.Git.Protect }, false),
	"allow-userns":    flag(func(f *File) **bool { return &f.Sandbox.AllowUserns }, true),
	"new-session":     legacySession(SessionOn),
	"no-new-session":  legacySession(SessionOff),
	"profile": func(f *File, r string) error {
		f.Profile.Name = &r
		return nil
	},
	"agent": func(f *File, r string) error {
		if r != "opencode" && r != "oc" {
			return fmt.Errorf("only opencode is supported; remove 'agent %s'", r)
		}
		return nil
	},
}

func flag(field func(*File) **bool, v bool) func(*File, string) error {
	return func(f *File, _ string) error {
		*field(f) = &v
		return nil
	}
}

func legacySession(m NewSessionMode) func(*File, string) error {
	return func(f *File, _ string) error {
		f.Sandbox.NewSession = &m
		return nil
	}
}

func legacyEnv(f *File, rest string) error {
	name, value, has := strings.Cut(rest, "=")
	if !has {
		f.Env.Pass = append(f.Env.Pass, name)
		return nil
	}
	if f.Env.Set == nil {
		f.Env.Set = map[string]string{}
	}
	f.Env.Set[name] = value
	return nil
}

// A comment starts at the beginning of a line or after whitespace; a '#'
// inside a path is kept.
var legacyComment = regexp.MustCompile(`(^|\s)#.*$`)

// legacyLines yields the directive and its argument for each meaningful line.
func legacyLines(data []byte, yield func(key, rest string) error) error {
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\r\n")
		line = strings.TrimSpace(legacyComment.ReplaceAllString(line, ""))
		if line == "" {
			continue
		}
		key, rest := line, ""
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			key, rest = line[:i], line[i+1:]
		}
		if err := yield(key, strings.TrimSpace(rest)); err != nil {
			return err
		}
	}
	return nil
}

var errStop = errors.New("stop")

// IsLegacy reports whether data looks like the old line-based format: its
// first directive is one the old format knows.
func IsLegacy(data []byte) bool {
	legacy := false
	legacyLines(data, func(key, _ string) error {
		_, legacy = legacyDirectives[key]
		return errStop
	})
	return legacy
}

// ConvertLegacy converts the old line-based format.  Unknown directives are
// errors so no setting is dropped silently.
func ConvertLegacy(data []byte) (*File, error) {
	f := &File{}
	err := legacyLines(data, func(key, rest string) error {
		apply, ok := legacyDirectives[key]
		if !ok {
			return fmt.Errorf("unknown directive '%s'", key)
		}
		return apply(f, rest)
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Migrate converts the old configuration file to config.toml in the same
// directory and returns the new path.  It never overwrites.
func Migrate(loc Locations) (string, error) {
	src := loc.Legacy
	if loc.Explicit {
		if data, err := os.ReadFile(loc.TOML); err == nil && IsLegacy(data) {
			src = loc.TOML
		}
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("no old configuration file to migrate: %w", err)
	}
	f, err := ConvertLegacy(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src, err)
	}
	dest := filepath.Join(filepath.Dir(src), "config.toml")
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# Migrated from %s by kandor --migrate-config.\n\n", src)
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(f); err != nil {
		return "", err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("%s already exists", dest)
	}
	if err != nil {
		return "", err
	}
	if _, err := out.Write(buf.Bytes()); err != nil {
		out.Close()
		return "", err
	}
	return dest, out.Close()
}
