package sandbox

import (
	"regexp"
	"strings"
)

var urlCredentials = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*://)[^/@]+@(.*)$`)

func redactedName(name string) bool {
	for _, s := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"} {
		if strings.Contains(name, s) {
			return true
		}
	}
	return strings.HasPrefix(name, "AWS_") || strings.HasPrefix(name, "AZURE_") ||
		name == "GOOGLE_APPLICATION_CREDENTIALS" || name == "SSH_AUTH_SOCK"
}

// FormatCommand renders the environment env and argv for --dry-run as an
// "env -i" command: secret-looking environment values and credentials
// embedded in URLs are redacted, and each word is shell-quoted only when it
// needs to be.
func FormatCommand(env, argv []string) string {
	var words []string
	if len(env) > 0 {
		words = append(words, "env", "-i")
	}
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if redactedName(name) {
			value = "<redacted>"
		} else if m := urlCredentials.FindStringSubmatch(value); m != nil {
			value = m[1] + "<redacted>@" + m[2]
		}
		words = append(words, name+"="+value)
	}
	words = append(words, argv...)
	for i, w := range words {
		words[i] = shellQuote(w)
	}
	return strings.Join(words, " ")
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(w string) string {
	if plainWord.MatchString(w) {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}
