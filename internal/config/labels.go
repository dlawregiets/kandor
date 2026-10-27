package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Effect says whether a setting's value makes the sandbox stricter or more
// permissive.  A future per-project configuration layer applies narrowing
// settings automatically and asks before applying widening ones.
type Effect int

const (
	Neutral Effect = iota
	Narrows
	Widens
)

func (e Effect) String() string {
	return [...]string{"neutral", "narrows", "widens"}[e]
}

type classifier func(v any) (Effect, error)

func boolean(ifTrue, ifFalse Effect) classifier {
	return func(v any) (Effect, error) {
		b, ok := v.(bool)
		if !ok {
			return Neutral, fmt.Errorf("want a boolean, got %T", v)
		}
		if b {
			return ifTrue, nil
		}
		return ifFalse, nil
	}
}

func text(e Effect) classifier {
	return func(v any) (Effect, error) {
		if _, ok := v.(string); !ok {
			return Neutral, fmt.Errorf("want a string, got %T", v)
		}
		return e, nil
	}
}

// exposure covers lists and tables that expose host paths or variables: any
// entry widens the sandbox, including read-only mounts.
func exposure(v any) (Effect, error) {
	n := 0
	switch x := v.(type) {
	case []string:
		n = len(x)
	case map[string]string:
		n = len(x)
	default:
		return Neutral, fmt.Errorf("want a list or table, got %T", v)
	}
	if n == 0 {
		return Neutral, nil
	}
	return Widens, nil
}

func session(v any) (Effect, error) {
	m, ok := v.(NewSessionMode)
	if !ok {
		return Neutral, fmt.Errorf("want \"auto\", true or false, got %T", v)
	}
	switch m {
	case SessionOn:
		return Narrows, nil
	case SessionOff:
		return Widens, nil
	}
	return Neutral, nil
}

var labels = map[string]classifier{
	"mounts.read_only":            exposure,
	"mounts.read_write":           exposure,
	"env.pass":                    exposure,
	"env.set":                     exposure,
	"sandbox.network":             boolean(Widens, Narrows),
	"sandbox.allow_userns":        boolean(Widens, Narrows),
	"sandbox.new_session":         session,
	"credentials.ssh":             boolean(Widens, Narrows),
	"credentials.ssh_agent":       boolean(Widens, Narrows),
	"credentials.git_credentials": boolean(Widens, Narrows),
	"credentials.netrc":           boolean(Widens, Narrows),
	"credentials.npmrc":           boolean(Widens, Narrows),
	"git.mount_config":            boolean(Widens, Narrows),
	"git.protect":                 boolean(Narrows, Widens),
	"profile.isolated":            boolean(Narrows, Widens),
	"profile.name":                text(Narrows),
	"profile.seed":                boolean(Neutral, Neutral),
	"agents.*.binary":             text(Widens),
	"agents.*.mounts.read_only":   exposure,
	"agents.*.mounts.read_write":  exposure,
	"agents.*.env.pass":           exposure,
	"agents.*.env.set":            exposure,
}

// Classify returns the effect of setting key to value.  Agent keys use the
// "agents.*" placeholder.
func Classify(key string, value any) (Effect, error) {
	c, ok := labels[key]
	if !ok {
		return Neutral, fmt.Errorf("unknown setting %q", key)
	}
	e, err := c(value)
	if err != nil {
		return Neutral, fmt.Errorf("%s: %w", key, err)
	}
	return e, nil
}

// Keys lists every setting in File, derived from its toml tags.
func Keys() []string {
	var keys []string
	walk(reflect.TypeFor[File](), "", &keys)
	sort.Strings(keys)
	return keys
}

func walk(t reflect.Type, prefix string, keys *[]string) {
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
		key := prefix + name
		switch {
		case f.Type.Kind() == reflect.Struct:
			walk(f.Type, key+".", keys)
		case f.Type.Kind() == reflect.Map && f.Type.Elem().Kind() == reflect.Struct:
			walk(f.Type.Elem(), key+".*.", keys)
		default:
			*keys = append(*keys, key)
		}
	}
}
