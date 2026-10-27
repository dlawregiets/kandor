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
# settings are errors.

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

# [agents.opencode.mounts]  # like [mounts], but only when running OpenCode
# read_only  = []
# read_write = []

# [agents.opencode.env]  # like [env], but only when running OpenCode
# pass = []

# [agents.opencode.env.set]
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
