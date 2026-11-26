package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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

// initProjectAgentLine is the commented default_agent line in the template.
const initProjectAgentLine = `# default_agent = "opencode"  # override the user configuration's agent here`

// initProject writes a .kandor.toml starter into projDir, prompting about
// each section defined in the user configuration.  Prompts go to stderr so
// stdout stays clean; an unreadable or EOF stdin answers no.  A non-empty
// agent is written as the uncommented default_agent, and the project file is
// kept in the project's .gitignore.
func initProject(projDir string, user *config.File, agent string, stdin io.Reader, stderr io.Writer) error {
	path := config.ProjectPath(projDir)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("project configuration already exists: %s", path)
	}
	if err := addGitignore(projDir, stderr); err != nil {
		return err
	}
	included := promptSections(user.Sections, stdin, stderr)
	content := fmt.Sprintf(initProjectTemplate, quoteAll(included))
	if agent != "" {
		line := fmt.Sprintf("default_agent = %q  # override the user configuration's agent here", agent)
		content = strings.Replace(content, initProjectAgentLine, line, 1)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("cannot write project configuration: %s", path)
	}
	fmt.Fprintf(stderr, "kandor: created %s\n", path)
	return nil
}

// addGitignore adds .kandor.toml to the project's .gitignore unless a line
// already covers it, creating .gitignore when the project has none.  Writing
// it first keeps a failed --init-project from leaving an unignored file.
func addGitignore(projDir string, stderr io.Writer) error {
	path := filepath.Join(projDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot read %s", path)
	}
	for line := range strings.Lines(string(data)) {
		entry := strings.TrimSpace(line)
		if entry == config.ProjectFileName || entry == "/"+config.ProjectFileName {
			return nil
		}
	}
	add := config.ProjectFileName + "\n"
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		add = "\n" + add
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("cannot write %s", path)
	}
	_, err = f.WriteString(add)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("cannot write %s", path)
	}
	fmt.Fprintf(stderr, "kandor: added %s to %s\n", config.ProjectFileName, path)
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
