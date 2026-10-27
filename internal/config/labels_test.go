package config

import (
	"slices"
	"testing"
)

var samples = []any{true, false, SessionAuto, SessionOn, "x", []string{"x"}, map[string]string{"X": "1"}}

func TestEveryKeyIsClassified(t *testing.T) {
	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("Keys() is empty")
	}
	for _, k := range keys {
		ok := false
		for _, v := range samples {
			if _, err := Classify(k, v); err == nil {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("setting %q has no label", k)
		}
	}
	for k := range labels {
		if !slices.Contains(keys, k) {
			t.Errorf("label for %q, which is not a setting", k)
		}
	}
}

func TestKeysCoverSchema(t *testing.T) {
	keys := Keys()
	for _, want := range []string{
		"sandbox.network", "sandbox.new_session", "credentials.ssh", "git.protect",
		"profile.name", "env.set", "mounts.read_only", "agents.*.mounts.read_write",
		"agents.*.binary", "agents.*.env.set",
	} {
		if !slices.Contains(keys, want) {
			t.Errorf("Keys() is missing %q; got %v", want, keys)
		}
	}
	if !slices.IsSorted(keys) {
		t.Error("Keys() is not sorted")
	}
}

func TestClassifyValues(t *testing.T) {
	cases := []struct {
		key   string
		value any
		want  Effect
	}{
		{"sandbox.network", false, Narrows},
		{"sandbox.network", true, Widens},
		{"sandbox.allow_userns", true, Widens},
		{"sandbox.new_session", SessionOff, Widens},
		{"sandbox.new_session", SessionAuto, Neutral},
		{"credentials.ssh", true, Widens},
		{"credentials.npmrc", false, Narrows},
		{"git.protect", false, Widens},
		{"git.protect", true, Narrows},
		{"profile.isolated", true, Narrows},
		{"profile.name", "work", Narrows},
		{"profile.seed", false, Neutral},
		{"mounts.read_write", []string{"x"}, Widens},
		{"mounts.read_only", []string{"x"}, Widens},
		{"mounts.read_only", []string{}, Neutral},
		{"env.set", map[string]string{"A": "1"}, Widens},
		{"agents.*.binary", "/x", Widens},
	}
	for _, c := range cases {
		got, err := Classify(c.key, c.value)
		if err != nil || got != c.want {
			t.Errorf("Classify(%q, %v) = %v, %v; want %v", c.key, c.value, got, err, c.want)
		}
	}
}

func TestClassifyUnknownKey(t *testing.T) {
	if _, err := Classify("sandbox.nope", true); err == nil {
		t.Error("unknown key accepted")
	}
	if _, err := Classify("sandbox.network", "yes"); err == nil {
		t.Error("wrong value type accepted")
	}
}
