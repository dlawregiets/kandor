# Typical setups


### Rust/Go/Node builds

Builds need their toolchains and caches. The toolchains in `/usr` are
read-only and available. Add the caches you need explicitly:

```sh
kandor \
  --rw ~/.cargo \
  --rw ~/go \
  --rw ~/.cache/go-build \
  --rw ~/.npm \
  run "build and test"
```

Only add directories you are comfortable exposing to the agent.

### Git push over SSH

```sh
kandor --ssh --ssh-agent -- run "commit and push"
```

`~/.ssh` is read-only and the agent socket is forwarded read-only. The agent
can use your keys but cannot read or modify the key files. If you use HTTPS
credentials, add `--git-credentials`.

### Local model or proxy on the host

The default host network allows reaching a local model server:

```sh
kandor --env OLLAMA_HOST=http://127.0.0.1:11434
```

Remember that this also exposes other host services on localhost.

### Offline

```sh
kandor --no-net
```

With no network, the agent cannot call a remote provider. Use a local model
server reachable without the network namespace, or run without `--no-net` and
restrict host services.

