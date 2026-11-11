package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const template = `# kandor configuration file
#
# Every setting is commented out and shows its default.  Delete the leading
# "# " to change one.  Command-line options override this file, and unknown
# settings are errors.  A ".kandor.toml" file in the project directory adds
# project-specific settings and an include list on top of this file; the
# "default" section below always applies.

# include = []  # named stacks from [sections.*] to always apply, e.g. ["go"]

# default_agent = "opencode"  # the agent to run; --agent and KANDOR_AGENT win,
#                             # and a project .kandor.toml overrides it there

# [mounts]
# read_only  = []  # host paths mounted read-only, e.g. ["~/.config/gcloud"]
# read_write = []  # host paths mounted read-write, e.g. ["~/go"]

# [env]
# pass = []  # variables copied from your environment, e.g. ["GITHUB_TOKEN"]

# [env.set]  # NAME = "value" pairs set inside the sandbox

# [sandbox]
# network      = true    # false: no network at all
# allow_userns = false   # true: allow nested user namespaces (browsers); weakens the sandbox
# new_session  = "auto"  # true or false: detach from the terminal (TIOCSTI protection; may stop resizing)

# [credentials]  # all off unless you turn them on
# ssh             = false  # mount ~/.ssh read-only
# ssh_agent       = false  # forward $SSH_AUTH_SOCK
# git_credentials = false  # mount ~/.git-credentials read-only
# netrc           = false  # mount ~/.netrc read-only
# npmrc           = false  # mount ~/.npmrc read-only

# [git]
# mount_config = true  # mount ~/.gitconfig and ~/.config/git read-only
# protect      = true  # keep .git/hooks and .git/config read-only

# [profile]
# isolated = false      # true: a sandbox-only profile instead of the host's OpenCode data
# name     = "default"  # isolated profile name; setting it implies isolated = true
# seed     = true       # copy the host OpenCode config into a new isolated profile

# [agents.opencode]
# binary = ""  # path to opencode; empty searches PATH (KANDOR_BIN overrides it)
# subcommands = []  # extra subcommands, for agents defined here

# [agents.opencode.paths]  # host state for the agent, in four modes
# share = []    # mounted read-write (warned)
# copy = []     # seeded into the profile once, then the copy is writable
# private = []  # fresh profile directories; matching host paths stay hidden
# ro = []       # mounted read-only
# cache = []    # kandor-owned persistent directories (never the host's)

# [agents.opencode.mounts]  # like [mounts], but only when running OpenCode
# read_only  = []
# read_write = []

# [agents.opencode.env]  # like [env], but only when running OpenCode
# pass = []
# exclude = []  # host variables this agent must never see

# [agents.opencode.env.set]

# [agents.pi]  # another agent works the same way; see docs/agents.md

# [sections.example]  # a reusable stack of settings, e.g. [sections.go].
# A project opts in with its own include list in .kandor.toml; the default
# stack, when defined, always applies.  Sections hold the same settings as
# the top level of this file.
# [sections.example.mounts]  # like [mounts], but only for includers
# read_only  = []
# read_write = []

# [sections.example.env]  # like [env], but only for includers
# pass = []

# [sections.example.env.set]

# [sections.example.sandbox]
# network      = true
# allow_userns = false
# new_session  = "auto"

# [sections.example.credentials]
# ssh             = false
# ssh_agent       = false
# git_credentials = false
# netrc           = false
# npmrc           = false

# [sections.example.git]
# mount_config = true
# protect      = true

# [sections.example.profile]
# isolated = false
# name     = "default"
# seed     = true

# [sections.example.agents.opencode]
# binary = ""
# subcommands = []

# [sections.example.agents.opencode.paths]
# share = []
# copy = []
# private = []
# ro = []

# [sections.example.paths]
# share = []
# copy = []
# private = []
# ro = []
# cache = []

# [sections.example.agents.opencode.mounts]
# read_only  = []
# read_write = []

# [sections.example.agents.opencode.env]
# pass = []
# exclude = []

# [sections.example.agents.opencode.env.set]
`

// Template returns a configuration file with every setting commented out.
func Template() string { return template }

// WriteTemplate writes Template() to path.  It never overwrites, so a typo in
// KANDOR_CONFIG cannot destroy the user's settings.
func WriteTemplate(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("configuration file already exists: %s", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create configuration directory: %s", dir)
	}
	os.Chmod(dir, 0o700)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("configuration file already exists: %s", path)
	}
	if err != nil {
		return fmt.Errorf("cannot write configuration file: %s", path)
	}
	if _, err := f.WriteString(template); err != nil {
		f.Close()
		return fmt.Errorf("cannot write configuration file: %s", path)
	}
	return f.Close()
}
