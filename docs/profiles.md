# Profiles


### Isolated profile (`--isolated`)

```sh
kandor --isolated           # profile "default"
kandor --profile work       # implies --isolated
kandor --profile work --no-seed
```

State lives in:

```
~/.local/share/kandor/<profile>/
├── config/opencode/
├── data/opencode/
├── state/opencode/
└── cache/opencode/
```

The first time a profile is used, `~/.config/opencode` is copied into
`config/opencode` once (except `service.json`). Stored logins live in the
data directory (`~/.local/share/opencode/auth.json`) and are not copied.
Note that `opencode.json` itself is copied, so API keys written directly into
it reach the profile.

This makes plugins and settings work without copying the host's sessions or
stored OAuth credentials. Delete the profile directory to start over, or use
`--no-seed` to start with an empty config. Do not run two sandboxes with the
same profile at the same time; use different `--profile` names
instead.

Environment variables of known LLM providers are forwarded automatically to
OpenCode: those starting with `OPENAI_`, `ANTHROPIC_`, `OPENROUTER_`,
`GEMINI_`, `GOOGLE_`, `VERTEX_`, `AWS_`, `AZURE_`, `DEEPSEEK_`, `XAI_`,
`GROQ_`, `MISTRAL_`, `COHERE_`, `PERPLEXITY_`, `TOGETHER_`, `FIREWORKS_`,
`CEREBRAS_`, `OLLAMA_`, `LMSTUDIO_`, `HF_`, `REPLICATE_`, `MOONSHOT_`,
`KIMI_`, `ZAI_`, `ZHIPU_`, `MINIMAX_`, `DASHSCOPE_` or `LITELLM_`, plus
proxy and CA settings. Note that `AWS_*`, `AZURE_*` and `GOOGLE_*` include
cloud credentials (for Bedrock, Azure OpenAI and Vertex). Other secrets, such
as `STRIPE_SECRET_KEY` or `GITHUB_TOKEN`, are not forwarded; pass them
explicitly with `--env NAME` if OpenCode needs them. In an isolated profile,
OAuth logins are stored in the profile; log in once per profile:

```sh
kandor --isolated auth login
```

### Shared profile (default)

```sh
kandor
kandor --shared              # same as the default, prints a warning
```

This shares the host's live `~/.local/share/opencode`
read-write: the session database and the stored provider logins in
`auth.json`. Config, state and cache are copied or created in a
temporary directory for the duration of the run; the host's cached `rg`
binary is mounted read-only so OpenCode does not have to download it again.
The host's `service.json` password and service registration stay outside the
sandbox, even when a mount includes their directories: kandor covers them
with the sandbox's private copies.

To keep OpenCode's config changes (models, plugins, agents), mount the host
directory read-write, for example `--rw ~/.config/opencode` or
`read_write = ["~/.config/opencode"]` under `[mounts]`. OpenCode then uses that
directory directly instead of a temporary copy. The same works for
`~/.local/state/opencode` and `~/.cache/opencode`. An isolated profile always
uses its own copy.

Sharing is useful for continuing host sessions, but it is **less isolated**:

- The host's session data (all projects, prompts and tool output) is
  readable inside the sandbox. Treat it as trusted with anything that has
  ever appeared in a host session; that can include credentials or the host
  service password if they were ever printed.
- The sandbox can read the stored provider logins in `auth.json` and modify
  host sessions. Config, plugin and cache changes are made in a temporary
  copy and discarded when the run exits.
- Anything the sandbox writes into the host data directory is read by
  OpenCode when you next run it on the host, outside the sandbox.
- Host OpenCode must not be running a conflicting migration; SQLite WAL
  generally allows concurrent access, but avoid concurrent heavy writes.

`kandor` warns whenever it shares host state (the default). Use
`--isolated` for untrusted projects.

