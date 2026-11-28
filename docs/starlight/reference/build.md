---
title: avr build
description: "Build container images remotely with docker buildx on Avrea builders."
---

Build container images remotely with docker buildx on Avrea builders.

```sh
avr build [OPTIONS] [BUILDX_ARGS]...
```

Mirrors `docker build`: every flag not listed below is forwarded verbatim
to `docker buildx build` running against buildkitd in an ephemeral Avrea
builder VM, so `-t`, `--push`, `--build-arg`, `-f`, `--output` etc. all
behave as they do locally. Registry credentials come from your local
`docker login` and are forwarded per build; Avrea never stores them.

Requires either a local docker CLI with buildx, or podman plus the
standalone docker-buildx binary; with both installed podman is used
unless --engine docker (or AVR_BUILD_ENGINE=docker) is given. With podman,
`--load` goes into podman's image store and `podman login` credentials are
used for --push. Also requires an authenticated `avr` client and the
OpenSSH client.

Without --platform the image is built for this machine's CPU
architecture, as `docker build` does: linux/arm64 on Apple silicon and
other ARM machines, linux/amd64 on x86. Pass --platform to build for
another architecture, such as linux/amd64 for an x86 server.

The repository (--repo, AVR_REPO, or auto-detected from the git remote)
only scopes the build cache. It is optional: without one, or when the
auto-detected repository isn't connected to the org, the build runs
without a repo-scoped cache.

With a repository, layers are cached in Avrea's GHA cache (BuildKit's
type=gha backend, mode=max), scoped per Dockerfile path, --target and
platform. The first build reads the default branch's CI cache; writes go
to a builder-only scope that CI never reads. Passing any --cache-from or
--cache-to replaces these defaults.

**Examples**

```sh
avr build -t myimage .
avr build --push -t registry.avrea.com/acme/app:latest .
avr build --platform linux/amd64,linux/arm64 --push -t acme/app .
avr build --size 8-vcpu --build-arg VERSION=1.2 -t app .
```

Like `docker build`, a single-platform build without --push or --output
is loaded into the local image store (docker, or podman through its API
socket); pass `--output type=cacheonly` to skip that. A `Containerfile`
in the build context is used when there is no `Dockerfile`. Like
`podman build`, the build context defaults to the current directory.

Multi-platform builds create one builder session per architecture and
require --push or --output (a manifest list cannot be loaded into the
local docker daemon).

The builder sessions are deleted when the build exits, also on Ctrl-C.
`avr builder list` shows sessions a killed client left behind.

**Arguments**

- <code class="cli-arg">[BUILDX_ARGS...]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;platform</code> <code class="cli-value">&lt;TEXT&gt;</code> — Target platform(s): linux/amd64 and/or linux/arm64, comma-separated or repeated. Each platform builds natively on its own builder VM. Defaults to this machine's architecture. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository the build belongs to (org/repo or rep-...); scopes the build cache. Optional — auto-detected from the git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;size</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Builder VM size (per platform). _(choices: `1-vcpu`, `2-vcpu`, `4-vcpu`, `8-vcpu`, `16-vcpu`, `32-vcpu` · default: `4-vcpu`)_
- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Builder session TTL, e.g. 30m or 2h. Default 1h, max 8h.
- <code class="cli-flag">&#x2D;&#x2D;keep-sessions</code> — Debug: leave the builder sessions running after the build instead of deleting them.
- <code class="cli-flag">&#x2D;&#x2D;provision-timeout</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Seconds to wait for builder VMs to become reachable. _(default: `300`)_
- <code class="cli-flag">&#x2D;&#x2D;engine</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Local container tooling: docker (docker CLI + buildx plugin) or podman (podman + standalone docker-buildx). auto prefers podman when both are available. Env: AVR_BUILD_ENGINE. _(choices: `auto`, `docker`, `podman` · default: `auto` · env: `AVR_BUILD_ENGINE`)_
