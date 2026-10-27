package sandbox

import (
	"os"
	"strings"

	"github.com/dlawregiets/kandor/internal/config"
)

// AddNamespaces adds the namespace and security options, the working
// directory and --clearenv.  disableUsernsWorks says whether this kernel
// accepts --disable-userns; newSession whether to detach from the terminal.
func AddNamespaces(p *Plan, s config.Settings, f Features, disableUsernsWorks, newSession bool, project string) {
	userns := "--unshare-user"
	if f.UnshareUserTry {
		userns = "--unshare-user-try"
	}
	if !s.AllowUserns && f.DisableUserns && disableUsernsWorks {
		p.Arg("--unshare-user", "--disable-userns")
	} else {
		p.Arg(userns)
	}
	p.Arg("--unshare-pid", "--unshare-ipc", "--unshare-uts")
	if f.UnshareCgroupTry {
		p.Arg("--unshare-cgroup-try")
	}
	if !s.Network {
		p.Arg("--unshare-net")
	}
	p.Arg("--hostname", Hostname)
	if newSession && f.NewSession {
		p.Arg("--new-session")
	}
	if f.DieWithParent {
		p.Arg("--die-with-parent")
	}
	if f.CapDrop {
		p.Arg("--cap-drop", "ALL")
	}
	p.Arg("--chdir", project, "--clearenv")
}

// TIOCSTIPath is where the kernel says whether TIOCSTI is allowed.
const TIOCSTIPath = "/proc/sys/dev/tty/legacy_tiocsti"

// NewSessionDefault resolves sandbox.new_session.  TIOCSTI lets a process
// inject keystrokes into the controlling terminal, so "auto" detaches from it
// unless the kernel has TIOCSTI disabled.  explicit is false for "auto".
func NewSessionDefault(mode config.NewSessionMode, tiocstiPath string) (on, explicit bool) {
	switch mode {
	case config.SessionOn:
		return true, true
	case config.SessionOff:
		return false, true
	}
	data, err := os.ReadFile(tiocstiPath)
	return err != nil || strings.TrimSpace(string(data)) != "0", false
}
