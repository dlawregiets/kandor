package sandbox

import (
	"fmt"
	"os/user"
	"regexp"
	"slices"
	"strings"

	"github.com/dlawregiets/kandor/internal/agent"
	"github.com/dlawregiets/kandor/internal/config"
)

// Hostname is the sandbox's hostname.
const Hostname = "kandor"

// EnvInput is everything Environment needs.
type EnvInput struct {
	Environ    []string // the host environment, as from os.Environ
	Home       string
	RuntimeDir string
	Layout     agent.Layout
	AgentEnv   []agent.EnvVar
	Exclude    func(name string) bool // agent-specific variables never forwarded
	Extra      []config.EnvSpec       // configured and --env variables
	// SSHAuthSock is the resolved agent socket, "" when not forwarded.
	SSHAuthSock string
	// GitConfigGlobal is the mounted global git config, "" if none.
	GitConfigGlobal string
	Warn            func(string)
}

// Handled explicitly, or host-specific, so never copied by the allowlist.
var envSkipped = []string{
	"HOME", "USER", "LOGNAME", "SHELL", "PATH", "TERM", "COLORTERM", "NO_COLOR", "FORCE_COLOR",
	"CLICOLOR", "CLICOLOR_FORCE", "TMPDIR", "TZ", "LANG", "LANGUAGE", "HOSTNAME", "PWD", "OLDPWD",
	"SHLVL", "_", "EDITOR", "VISUAL", "SSH_AUTH_SOCK", "DBUS_SESSION_BUS_ADDRESS", "DISPLAY",
	"WAYLAND_DISPLAY", "XAUTHORITY",
}

// Only the prefixes of known LLM providers (and the cloud platforms that host
// them) are forwarded, so unrelated secrets such as STRIPE_SECRET_KEY stay
// outside.  Pass anything else with --env.
var providerPrefixes = []string{
	"OPENAI_", "ANTHROPIC_", "OPENROUTER_", "GEMINI_", "GOOGLE_", "VERTEX_", "AWS_", "AZURE_",
	"DEEPSEEK_", "XAI_", "GROQ_", "MISTRAL_", "COHERE_", "PERPLEXITY_", "TOGETHER_", "FIREWORKS_",
	"CEREBRAS_", "OLLAMA_", "LMSTUDIO_", "LM_STUDIO_", "HF_", "HUGGING_FACE_", "REPLICATE_",
	"MOONSHOT_", "KIMI_", "ZAI_", "ZHIPU_", "MINIMAX_", "DASHSCOPE_", "LITELLM_",
}

var envForwarded = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE",
	"GIT_SSL_CAINFO", "GIT_SSL_CAPATH", "AWS_CA_BUNDLE", "GIT_EDITOR", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
}

var validEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Environment returns the variables to set inside the sandbox, in order (a
// later variable of the same name wins).  bwrap starts from an empty
// environment, so nothing else reaches the sandbox.
func Environment(in EnvInput) ([]agent.EnvVar, error) {
	host := map[string]string{}
	var names []string
	for _, kv := range in.Environ {
		name, value, _ := strings.Cut(kv, "=")
		if _, dup := host[name]; !dup {
			names = append(names, name)
		}
		host[name] = value
	}
	get := func(name, fallback string) string {
		if v := host[name]; v != "" {
			return v
		}
		return fallback
	}

	path := get("PATH", "/usr/local/bin:/usr/bin:/bin")
	if !strings.Contains(":"+path+":", ":/usr/bin:") {
		path = "/usr/bin:/bin:" + path
	}
	userName := get("USER", "")
	if userName == "" {
		if u, err := user.Current(); err == nil {
			userName = u.Username
		}
	}

	vars := []agent.EnvVar{
		{Name: "HOME", Value: in.Home},
		{Name: "USER", Value: userName},
		{Name: "LOGNAME", Value: get("LOGNAME", userName)},
		{Name: "SHELL", Value: get("SHELL", "/bin/bash")},
		{Name: "PATH", Value: path},
		{Name: "HOSTNAME", Value: Hostname},
		{Name: "TERM", Value: get("TERM", "xterm-256color")},
		{Name: "TMPDIR", Value: "/tmp"},
		{Name: "XDG_CONFIG_HOME", Value: in.Layout.Config},
		{Name: "XDG_DATA_HOME", Value: in.Layout.Data},
		{Name: "XDG_STATE_HOME", Value: in.Layout.State},
		{Name: "XDG_CACHE_HOME", Value: in.Layout.Cache},
		{Name: "XDG_RUNTIME_DIR", Value: in.RuntimeDir},
		{Name: "LANG", Value: get("LANG", "C.UTF-8")},
	}
	vars = append(vars, in.AgentEnv...)
	for _, name := range []string{"COLORTERM", "NO_COLOR", "FORCE_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "TZ", "EDITOR", "VISUAL"} {
		if v := host[name]; v != "" {
			vars = append(vars, agent.EnvVar{Name: name, Value: v})
		}
	}
	if in.SSHAuthSock != "" {
		vars = append(vars, agent.EnvVar{Name: "SSH_AUTH_SOCK", Value: in.SSHAuthSock})
	}

	slices.Sort(names)
	for _, name := range names {
		if slices.Contains(envSkipped, name) || strings.HasPrefix(name, "LC_") ||
			strings.HasPrefix(name, "XDG_") || (in.Exclude != nil && in.Exclude(name)) {
			continue
		}
		if slices.Contains(envForwarded, name) || slices.ContainsFunc(providerPrefixes, func(p string) bool {
			return strings.HasPrefix(name, p)
		}) {
			vars = append(vars, agent.EnvVar{Name: name, Value: host[name]})
		}
	}

	if in.GitConfigGlobal != "" {
		vars = append(vars, agent.EnvVar{Name: "GIT_CONFIG_GLOBAL", Value: in.GitConfigGlobal})
	}
	for _, spec := range in.Extra {
		if spec.Name == "" {
			continue
		}
		value := spec.Value
		if !spec.HasValue {
			v, ok := host[spec.Name]
			if !ok {
				in.Warn(fmt.Sprintf("environment variable %s is not set; skipping", spec.Name))
				continue
			}
			value = v
		}
		if !validEnvName.MatchString(spec.Name) {
			return nil, fmt.Errorf("invalid environment variable name: %s", spec.Name)
		}
		vars = append(vars, agent.EnvVar{Name: spec.Name, Value: value})
	}
	return vars, nil
}
