package sandbox

import "strings"

// Features are the optional bwrap options this bwrap supports.
type Features struct {
	UnshareUserTry   bool
	DisableUserns    bool
	Chmod            bool
	UnshareCgroupTry bool
	NewSession       bool
	DieWithParent    bool
	CapDrop          bool
}

// ParseFeatures detects options from "bwrap --help" output.
func ParseFeatures(help string) Features {
	has := func(opt string) bool { return strings.Contains(help, opt) }
	return Features{
		UnshareUserTry:   has("--unshare-user-try"),
		DisableUserns:    has("--disable-userns"),
		Chmod:            has("--chmod"),
		UnshareCgroupTry: has("--unshare-cgroup-try"),
		NewSession:       has("--new-session"),
		DieWithParent:    has("--die-with-parent"),
		CapDrop:          has("--cap-drop"),
	}
}
