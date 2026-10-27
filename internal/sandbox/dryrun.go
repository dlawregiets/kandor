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

// FormatCommand renders argv for --dry-run: secret-looking environment
// values and credentials embedded in URLs are redacted, and each word is
// shell-quoted only when it needs to be.
func FormatCommand(argv []string) string {
	words := make([]string, len(argv))
	copy(words, argv)
	for i := 0; i+2 < len(words); i++ {
		if words[i] != "--setenv" {
			continue
		}
		if redactedName(words[i+1]) {
			words[i+2] = "<redacted>"
		} else if m := urlCredentials.FindStringSubmatch(words[i+2]); m != nil {
			words[i+2] = m[1] + "<redacted>@" + m[2]
		}
		i += 2
	}
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
