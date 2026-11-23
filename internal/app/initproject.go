package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/dlawregiets/kandor/internal/config"
)

// initProjectTemplate is the starting point written by --init-project.  Only
// section names go in, so the file is safe to share; keep secrets out of
// project files that might be committed.
const initProjectTemplate = `# Project configuration for kandor. It layers project-specific settings on
# top of the user configuration; reusable "sections" may only be defined in
# the user file. See docs/configuration.md.

# default_agent = "opencode"  # override the user configuration's agent here

include = [%s]
`

// initProject writes a .kandor.toml starter into projDir, prompting about
// each section defined in the user configuration.  Prompts go to stderr so
// stdout stays clean; an unreadable or EOF stdin answers no.
func initProject(projDir string, user *config.File, stdin io.Reader, stderr io.Writer) error {
	path := config.ProjectPath(projDir)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("project configuration already exists: %s", path)
	}
	included := promptSections(user.Sections, stdin, stderr)
	content := fmt.Sprintf(initProjectTemplate, quoteAll(included))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("cannot write project configuration: %s", path)
	}
	fmt.Fprintf(stderr, "kandor: created %s\n", path)
	return nil
}

// promptSections asks about each named section in sorted order and returns
// the included names.
func promptSections(sections map[string]config.Section, stdin io.Reader, stderr io.Writer) []string {
	names := make([]string, 0, len(sections))
	for name := range sections {
		// The "default" section always applies; including it is a no-op.
		if name == "default" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Fprintln(stderr, "kandor: no optional sections defined in the user configuration; writing an empty include list")
		return nil
	}
	fmt.Fprintln(stderr, "kandor: sections in the user configuration:")
	for _, name := range names {
		fmt.Fprintf(stderr, "kandor:   %s: %s\n", name, describeSection(sections[name]))
	}
	sc := bufio.NewScanner(stdin)
	var included []string
	for _, name := range names {
		fmt.Fprintf(stderr, "kandor: include %q? [y/N] ", name)
		yes := sc.Scan() && isYes(sc.Text())
		if yes {
			included = append(included, name)
		}
	}
	return included
}

func isYes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}

// describeSection summarizes a section in one line for the include prompt.
func describeSection(s config.Section) string {
	var parts []string
	if n := len(s.Mounts.ReadOnly) + len(s.Mounts.ReadWrite); n > 0 {
		parts = append(parts, plural(n, "mount"))
	}
	if n := len(s.Env.Pass) + len(s.Env.Set); n > 0 {
		parts = append(parts, plural(n, "environment variable"))
	}
	rest := s
	rest.Mounts = config.MountLists{}
	rest.Env = config.EnvSection{}
	if !reflect.DeepEqual(rest, config.Section{}) {
		parts = append(parts, "other settings")
	}
	if len(parts) == 0 {
		return "no settings"
	}
	return strings.Join(parts, ", ")
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}
