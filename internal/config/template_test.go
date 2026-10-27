package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTemplateLoadsAsEmpty(t *testing.T) {
	f, err := Load(writeFile(t, Template()))
	if err != nil {
		t.Fatalf("Load(Template()) error: %v", err)
	}
	if !reflect.DeepEqual(f, &File{}) {
		t.Errorf("Template() sets values: %+v", f)
	}
}

func TestTemplateUncommentedIsValid(t *testing.T) {
	// Uncommenting every line must give a valid file with the defaults.
	var b strings.Builder
	for line := range strings.Lines(Template()) {
		if strings.HasPrefix(line, "# ") && (strings.Contains(line, " = ") || strings.HasPrefix(line, "# [")) {
			line = strings.TrimPrefix(line, "# ")
		}
		b.WriteString(line)
	}
	f, err := Load(writeFile(t, b.String()))
	if err != nil {
		t.Fatalf("uncommented template does not load: %v\n%s", err, b.String())
	}
	s, err := Merge(f, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := Merge(&File{}, Overrides{}, "opencode", "/h", "/cwd", env(nil))
	if !reflect.DeepEqual(s, d) {
		t.Errorf("uncommented template differs from defaults:\n%+v\n%+v", s, d)
	}
}

func TestTemplateMentionsEveryKey(t *testing.T) {
	tmpl := Template()
	for _, k := range Keys() {
		k = strings.Replace(k, "agents.*", "agents.opencode", 1)
		section, name := k[:strings.LastIndex(k, ".")], k[strings.LastIndex(k, ".")+1:]
		if name == "set" {
			if !strings.Contains(tmpl, "["+k+"]") {
				t.Errorf("Template() has no [%s] section", k)
			}
			continue
		}
		if !strings.Contains(tmpl, "["+section+"]") || !strings.Contains(tmpl, "# "+name+" ") {
			t.Errorf("Template() does not show %s", k)
		}
	}
}

func TestWriteTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := WriteTemplate(path); err != nil {
		t.Fatalf("WriteTemplate error: %v", err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(filepath.Dir(path)); st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", st.Mode().Perm())
	}
}

func TestWriteTemplateRefusesOverwrite(t *testing.T) {
	path := writeFile(t, "# mine\n")
	err := WriteTemplate(path)
	if err == nil || err.Error() != "configuration file already exists: "+path {
		t.Errorf("err = %v", err)
	}
}
