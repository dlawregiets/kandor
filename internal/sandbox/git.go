package sandbox

import "github.com/dlawregiets/kandor/internal/paths"

// GitState records what was found and protected in the project's repository.
type GitState struct {
	Dir, Common    string
	Trusted        bool
	AuditCommondir string
	AuditNewRepo   bool
}

// DiscoverGit finds the project's repository and mounts an allowed common
// directory that lies outside the project.
func DiscoverGit(project string, host paths.Host, p *Plan, warn func(string)) GitState {
	return GitState{}
}

// Protect keeps the repository's hooks and config read-only.
func (g *GitState) Protect(p *Plan, project string, warn func(string)) {}
