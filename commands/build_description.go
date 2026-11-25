// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func buildDescription(path string) docgen.Description {
	if path != "avr build" {
		return docgen.Description{}
	}
	return docgen.Description{
		Help: "Build container images remotely with docker buildx on Avrea builders.\n" +
			"\n" +
			"Mirrors `docker build`: every flag not listed below is forwarded verbatim\n" +
			"to `docker buildx build` running against buildkitd in an ephemeral Avrea\n" +
			"builder VM, so `-t`, `--push`, `--build-arg`, `-f`, `--output` etc. all\n" +
			"behave as they do locally. Registry credentials come from your local\n" +
			"`docker login` and are forwarded per build; Avrea never stores them.\n" +
			"\n" +
			"Requires either a local docker CLI with buildx, or podman plus the\n" +
			"standalone docker-buildx binary; with both installed podman is used\n" +
			"unless --engine docker (or AVR_BUILD_ENGINE=docker) is given. With podman,\n" +
			"`--load` goes into podman's image store and `podman login` credentials are\n" +
			"used for --push. Also requires an authenticated `avr` client and the\n" +
			"OpenSSH client.\n" +
			"\n" +
			"Without --platform the image is built for this machine's CPU\n" +
			"architecture, as `docker build` does: linux/arm64 on Apple silicon and\n" +
			"other ARM machines, linux/amd64 on x86. Pass --platform to build for\n" +
			"another architecture, such as linux/amd64 for an x86 server.\n" +
			"\n" +
			"The repository (--repo, AVR_REPO, or auto-detected from the git remote)\n" +
			"only scopes the build cache. It is optional: without one, or when the\n" +
			"auto-detected repository isn't connected to the org, the build runs\n" +
			"without a repo-scoped cache.\n" +
			"\n" +
			"With a repository, layers are cached in Avrea's GHA cache (BuildKit's\n" +
			"type=gha backend, mode=max), scoped per Dockerfile path, --target and\n" +
			"platform. The first build reads the default branch's CI cache; writes go\n" +
			"to a builder-only scope that CI never reads. Passing any --cache-from or\n" +
			"--cache-to replaces these defaults.\n" +
			"\n" +
			"\b\n" +
			"Examples:\n" +
			"    avr build -t myimage .\n" +
			"    avr build --push -t registry.avrea.com/acme/app:latest .\n" +
			"    avr build --platform linux/amd64,linux/arm64 --push -t acme/app .\n" +
			"    avr build --size 8-vcpu --build-arg VERSION=1.2 -t app .\n" +
			"\n" +
			"Like `docker build`, a single-platform build without --push or --output\n" +
			"is loaded into the local image store (docker, or podman through its API\n" +
			"socket); pass `--output type=cacheonly` to skip that. A `Containerfile`\n" +
			"in the build context is used when there is no `Dockerfile`. Like\n" +
			"`podman build`, the build context defaults to the current directory.\n" +
			"\n" +
			"Multi-platform builds create one builder session per architecture and\n" +
			"require --push or --output (a manifest list cannot be loaded into the\n" +
			"local docker daemon).\n" +
			"\n" +
			"The builder sessions are deleted when the build exits, also on Ctrl-C.\n" +
			"`avr builder list` shows sessions a killed client left behind.\n",
		Short:          "Build container images remotely with docker buildx on Avrea builders.",
		CompletionHelp: "Build container images remotely with...",
		Usage:          []string{"[OPTIONS]", "[BUILDX_ARGS]..."},
		Parameters: []docgen.Parameter{
			{Param: docgen.Param{Name: "buildx_args", Kind: "argument", Opts: []string{"buildx_args"}, Type: "text", Nargs: -1}, CompletionKind: "Unprocessed"},
			{Param: docgen.Param{Name: "platforms", Kind: "option", Opts: []string{"--platform"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Target platform(s): linux/amd64 and/or linux/arm64, comma-separated or repeated. Each platform builds natively on its own builder VM. Defaults to this machine's architecture.")}, CompletionKind: "String"},
			{Param: docgen.Param{Name: "repo", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository the build belongs to (org/repo or rep-...); scopes the build cache. Optional — auto-detected from the git remote if omitted.")}, CompletionKind: "String"},
			{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
			{Param: docgen.Param{Name: "size", Kind: "option", Opts: []string{"--size"}, Type: "choice", Nargs: 1, Help: descriptionText("Builder VM size (per platform)."), ShowDefault: true, Default: "4-vcpu", Choices: buildSizes}, Suffix: "  [default: 4-vcpu]", CompletionKind: "Choice", CaseSensitive: true},
			{Param: docgen.Param{Name: "ttl", Kind: "option", Opts: []string{"--ttl"}, Type: "text", Nargs: 1, Help: descriptionText("Builder session TTL, e.g. 30m or 2h. Default 1h, max 8h.")}, CompletionKind: "String"},
			{Param: docgen.Param{Name: "keep_sessions", Kind: "option", Opts: []string{"--keep-sessions"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Debug: leave the builder sessions running after the build instead of deleting them."), Default: false}, CompletionKind: "Bool"},
			{Param: docgen.Param{Name: "provision_timeout", Kind: "option", Opts: []string{"--provision-timeout"}, Type: "integer", Nargs: 1, Help: descriptionText("Seconds to wait for builder VMs to become reachable."), ShowDefault: true, Default: 300}, Suffix: "  [default: 300]", CompletionKind: "Int"},
			{Param: docgen.Param{Name: "engine", Kind: "option", Opts: []string{"--engine"}, Type: "choice", Nargs: 1, Envvar: "AVR_BUILD_ENGINE", Help: descriptionText("Local container tooling: docker (docker CLI + buildx plugin) or podman (podman + standalone docker-buildx). auto prefers podman when both are available. Env: AVR_BUILD_ENGINE."), ShowDefault: true, Default: "auto", Choices: buildEngines}, Suffix: "  [default: auto]", CompletionKind: "Choice", CaseSensitive: true},
		},
	}
}
