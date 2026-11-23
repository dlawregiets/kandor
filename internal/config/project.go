// Per-project configuration: a ".kandor.toml" file in the project directory
// layers project-specific settings between the user configuration and the
// command line.  It accepts the same keys as the user file (plus "include"
// to opt into the user's named sections), except that reusable "sections"
// may only be defined in the user file.  Because a borrowed repository
// should not silently widen its own sandbox, keys in the project file that
// widen the sandbox are reported by WideningKeys so the caller can warn.
package config

import "path/filepath"

// ProjectFileName is the per-project configuration file, discovered in the
// project directory itself.
const ProjectFileName = ".kandor.toml"

// ProjectPath returns the per-project configuration file for a project
// directory.
func ProjectPath(projectDir string) string {
	return filepath.Join(projectDir, ProjectFileName)
}

// LoadProject loads the project file for a project directory.  A missing
// file yields an empty File and ok == false; other errors (including a
// legacy-format file) are reported.
func LoadProject(projectDir string) (proj *File, ok bool, err error) {
	path := ProjectPath(projectDir)
	f, err := Load(path)
	if err != nil {
		return nil, false, err
	}
	if !exists(path) {
		return f, false, nil
	}
	return f, true, nil
}

// WideningKeys lists the set keys in a project file that widen the sandbox,
// in schema order.  Section contents need no listing: they come from the
// user's own file, so including them is trusted.
func WideningKeys(f *File, agent string) []string {
	var out []string
	add := func(key string, v any) {
		if e, err := Classify(key, v); err == nil && e == Widens {
			out = append(out, key)
		}
	}
	add("mounts.read_only", f.Mounts.ReadOnly)
	add("mounts.read_write", f.Mounts.ReadWrite)
	add("env.pass", f.Env.Pass)
	add("env.set", f.Env.Set)
	if f.Sandbox.Network != nil {
		add("sandbox.network", *f.Sandbox.Network)
	}
	if f.Sandbox.AllowUserns != nil {
		add("sandbox.allow_userns", *f.Sandbox.AllowUserns)
	}
	if f.Sandbox.NewSession != nil {
		add("sandbox.new_session", *f.Sandbox.NewSession)
	}
	if f.Credentials.SSH != nil {
		add("credentials.ssh", *f.Credentials.SSH)
	}
	if f.Credentials.SSHAgent != nil {
		add("credentials.ssh_agent", *f.Credentials.SSHAgent)
	}
	if f.Credentials.GitCredentials != nil {
		add("credentials.git_credentials", *f.Credentials.GitCredentials)
	}
	if f.Credentials.Netrc != nil {
		add("credentials.netrc", *f.Credentials.Netrc)
	}
	if f.Credentials.Npmrc != nil {
		add("credentials.npmrc", *f.Credentials.Npmrc)
	}
	if f.Git.MountConfig != nil {
		add("git.mount_config", *f.Git.MountConfig)
	}
	if f.Git.Protect != nil {
		add("git.protect", *f.Git.Protect)
	}
	if f.Profile.Isolated != nil {
		add("profile.isolated", *f.Profile.Isolated)
	}
	if f.Profile.Name != nil {
		add("profile.name", *f.Profile.Name)
	}
	if a, ok := f.Agents[agent]; ok {
		prefix := "agents." + agent + "."
		addAs := func(key, display string, v any) {
			if e, err := Classify(key, v); err == nil && e == Widens {
				out = append(out, display)
			}
		}
		if a.Binary != nil {
			addAs("agents.*.binary", prefix+"binary", *a.Binary)
		}
		addAs("agents.*.mounts.read_only", prefix+"mounts.read_only", a.Mounts.ReadOnly)
		addAs("agents.*.mounts.read_write", prefix+"mounts.read_write", a.Mounts.ReadWrite)
		addAs("agents.*.env.pass", prefix+"env.pass", a.Env.Pass)
		addAs("agents.*.env.set", prefix+"env.set", a.Env.Set)
		addAs("agents.*.paths.share", prefix+"paths.share", a.Paths.Share)
		addAs("agents.*.paths.copy", prefix+"paths.copy", a.Paths.Copy)
		addAs("agents.*.paths.private", prefix+"paths.private", a.Paths.Private)
		addAs("agents.*.paths.ro", prefix+"paths.ro", a.Paths.RO)
		addAs("agents.*.paths.cache", prefix+"paths.cache", a.Paths.Cache)
	}
	return out
}
