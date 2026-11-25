package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
)

// ToolCommand is a local helper program for Dependencies.RunTool.
type ToolCommand struct {
	Args []string
	// Env replaces the inherited environment when it is not nil.
	Env []string
	// Stream attaches the program to the command's input and output instead
	// of capturing what it prints.
	Stream bool
}

// String shows the command line only: the environment may hold secrets.
func (c ToolCommand) String() string { return strings.Join(c.Args, " ") }

// ToolResult holds a finished program's exit code and, unless it was
// streamed, its output.
type ToolResult struct {
	Code           int
	Stdout, Stderr string
}

// executeTool is the default Dependencies.RunTool. An error means the program
// did not run to an exit code of its own.
func executeTool(ctx context.Context, d Dependencies, command ToolCommand) (ToolResult, error) {
	child := exec.CommandContext(ctx, command.Args[0], command.Args[1:]...)
	child.Env = command.Env
	var stdout, stderr bytes.Buffer
	if command.Stream {
		child.Stdin, child.Stdout, child.Stderr = d.In, d.Out, d.Err
		// A build is asked to cancel before it is killed.
		child.Cancel = func() error {
			if err := child.Process.Signal(os.Interrupt); err != nil {
				return child.Process.Kill()
			}
			return nil
		}
		child.WaitDelay = 10 * time.Second
	} else {
		child.Stdout, child.Stderr = &stdout, &stderr
		child.WaitDelay = 2 * time.Second
	}
	err := child.Run()
	result := ToolResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.Code = vmSSHExitCode(exit)
		return result, nil
	}
	return result, err
}

// buildEngine is the local tooling that drives the remote builder.
type buildEngine struct {
	name string
	// buildx is the argv prefix: `docker buildx`, or a standalone binary.
	buildx []string
	// dockerHost is podman's docker-compatible socket as a DOCKER_HOST URL.
	dockerHost string
	// env is the environment of every buildx call; nil inherits. Create,
	// build and rm must share it to find the same builder.
	env []string
}

var buildEngines = []string{"auto", "docker", "podman"}

const buildxInstallHint = "install the standalone docker-buildx binary (e.g. `brew install docker-buildx` or your distro's package)"

// Where docker packaging puts the buildx plugin. Podman users usually have
// the standalone binary in one of these, or on PATH.
var buildxPluginDirectories = []string{
	"~/.docker/cli-plugins",
	"/opt/homebrew/lib/docker/cli-plugins",
	"/usr/local/lib/docker/cli-plugins",
	"/usr/local/libexec/docker/cli-plugins",
	"/usr/lib/docker/cli-plugins",
	"/usr/libexec/docker/cli-plugins",
}

func buildExpandHome(path string) string {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}

// buildFindBuildx returns a buildx binary that runs without the docker CLI.
func buildFindBuildx(d Dependencies) string {
	for _, name := range []string{"docker-buildx", "buildx"} {
		if found, err := d.LookPath(name); err == nil {
			return found
		}
	}
	directories := buildxPluginDirectories
	if configured := d.Getenv("DOCKER_CONFIG"); configured != "" {
		directories = append([]string{filepath.Join(configured, "cli-plugins")}, directories...)
	}
	for _, directory := range directories {
		// LookPath also checks that a path is an executable file.
		if found, err := d.LookPath(filepath.Join(buildExpandHome(directory), "docker-buildx")); err == nil {
			return found
		}
	}
	return ""
}

// buildPodmanHost returns podman's docker-compatible API socket as a
// DOCKER_HOST URL, or "" when podman serves none (machine stopped, socket
// unit off).
func buildPodmanHost(ctx context.Context, d Dependencies) string {
	args := []string{"podman", "info", "--format", "{{.Host.RemoteSocket.Path}}"}
	switch runtime.GOOS {
	case "darwin":
		args = []string{"podman", "machine", "inspect", "--format", "{{.ConnectionInfo.PodmanSocket.Path}}"}
	case "windows":
		// PodmanSocket is the machine's socket inside WSL or Hyper-V; the
		// host reaches the API through the named pipe.
		args = []string{"podman", "machine", "inspect", "--format", "{{.ConnectionInfo.PodmanPipe.Path}}"}
	}
	result, err := d.RunTool(ctx, ToolCommand{Args: args})
	if err != nil || result.Code != 0 {
		return ""
	}
	path := strings.TrimPrefix(strings.TrimSpace(result.Stdout), "unix://")
	if path == "" {
		return ""
	}
	if pipe, ok := strings.CutPrefix(path, `\\.\pipe\`); ok {
		return "npipe:////./pipe/" + pipe
	}
	if _, err = os.Stat(path); err != nil {
		return ""
	}
	return "unix://" + path
}

func buildToolFailure(format string, values ...any) error {
	return &ExitError{1, "Error: " + fmt.Sprintf(format, values...)}
}

// buildResolveEngine picks the local tooling (auto prefers podman) and checks
// that it runs, so a missing tool fails before any session is created.
func buildResolveEngine(ctx context.Context, d Dependencies, choice string, args buildArgs) (*buildEngine, error) {
	_, podmanErr := d.LookPath("podman")
	hasPodman := podmanErr == nil
	_, dockerErr := d.LookPath("docker")
	hasDocker := dockerErr == nil
	standalone := ""
	if hasPodman || choice == "podman" {
		standalone = buildFindBuildx(d)
	}
	auto := choice == "auto"
	if auto {
		switch {
		case hasPodman && standalone != "":
			choice = "podman"
		case hasDocker:
			if hasPodman {
				if _, err := fmt.Fprintln(d.Err, "Note: podman found but no standalone buildx; using docker ("+buildxInstallHint+" to use podman)."); err != nil {
					return nil, err
				}
			}
			choice = "docker"
		case hasPodman:
			return nil, buildToolFailure("podman found but avr build also needs buildx: %s.", buildxInstallHint)
		default:
			return nil, buildToolFailure("neither docker nor podman found. avr build drives a local buildx: install Docker (the docker CLI with the buildx plugin), or podman plus the standalone docker-buildx binary.")
		}
	}
	if choice == "podman" {
		if !hasPodman {
			return nil, buildToolFailure("podman CLI not found; install podman or use --engine docker.")
		}
		if standalone == "" {
			return nil, buildToolFailure("podman builds need buildx: %s.", buildxInstallHint)
		}
		engine := &buildEngine{name: "podman", buildx: []string{standalone}}
		if d.Getenv("DOCKER_HOST") == "" {
			engine.dockerHost = buildPodmanHost(ctx, d)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		switch {
		case engine.dockerHost != "" || d.Getenv("DOCKER_HOST") != "":
		case auto && hasDocker && args.wantsLocalLoad():
			// A stopped podman machine must not fail a build that docker can
			// load, as with Docker Desktop next to an idle podman.
			if _, err := fmt.Fprintln(d.Err, "Note: podman's API socket is not available; using docker to load the image."); err != nil {
				return nil, err
			}
			return buildUseEngine(ctx, d, &buildEngine{name: "docker", buildx: []string{"docker", "buildx"}})
		case args.wantsLocalLoad():
			return nil, buildToolFailure("loading the image into podman (--load, the default without --push/--output) needs podman's API socket, which is not available. Start it (`podman machine start`, or `systemctl --user enable --now podman.socket` on Linux), or pass --push / --output.")
		default:
			if _, err := fmt.Fprintln(d.Err, "Warning: podman API socket not found; --load will not work (--push is unaffected)."); err != nil {
				return nil, err
			}
		}
		return buildUseEngine(ctx, d, engine)
	}
	return buildUseEngine(ctx, d, &buildEngine{name: "docker", buildx: []string{"docker", "buildx"}})
}

// buildUseEngine checks that engine's buildx runs and announces it.
func buildUseEngine(ctx context.Context, d Dependencies, engine *buildEngine) (*buildEngine, error) {
	if err := buildCheckBuildx(ctx, d, engine); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(d.Err, "Using %s (%s)\n", engine.name, engine.buildx[0]); err != nil {
		return nil, err
	}
	return engine, nil
}

// buildCheckBuildx checks that the chosen buildx runs. Its version output must
// name buildx: the podman-docker shim answers `docker buildx version` with
// podman's own buildah-backed alias, which cannot drive a remote builder.
func buildCheckBuildx(ctx context.Context, d Dependencies, engine *buildEngine) error {
	result, err := d.RunTool(ctx, ToolCommand{Args: append(append([]string{}, engine.buildx...), "version")})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if engine.name == "podman" {
		if err != nil {
			return buildToolFailure("%s could not be run; %s.", engine.buildx[0], buildxInstallHint)
		}
		if result.Code != 0 || !strings.Contains(result.Stdout, "buildx") {
			return buildToolFailure("%s did not run as buildx; reinstall it (%s).", engine.buildx[0], buildxInstallHint)
		}
		return nil
	}
	if err != nil {
		return buildToolFailure("docker CLI not found. avr build drives a local `docker buildx`; install Docker (or the docker CLI with the buildx plugin), or use podman with --engine podman.")
	}
	if result.Code != 0 || !strings.Contains(result.Stdout, "buildx") {
		return buildToolFailure("docker buildx is not available. Install the buildx plugin (Docker Desktop ships it by default).")
	}
	return nil
}

// buildPodmanAuthFiles lists podman's registry credential files that exist,
// in podman's lookup order: a registry missing from one file is looked up in
// the next.
func buildPodmanAuthFiles(d Dependencies) []string {
	candidates := []string{}
	if path := d.Getenv("REGISTRY_AUTH_FILE"); path != "" {
		candidates = append(candidates, path)
	}
	if directory := d.Getenv("XDG_RUNTIME_DIR"); directory != "" {
		candidates = append(candidates, filepath.Join(directory, "containers", "auth.json"))
	}
	configHome := d.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = buildExpandHome("~/.config")
	}
	candidates = append(candidates, filepath.Join(configHome, "containers", "auth.json"))
	found := []string{}
	for _, path := range candidates {
		if isRegularFile(path) && !slices.Contains(found, path) {
			found = append(found, path)
		}
	}
	return found
}

// buildPodmanAuth combines podman's credential files into one, the earlier
// file winning for each registry.
func buildPodmanAuth(d Dependencies) map[string]any {
	combined := map[string]any{}
	files := buildPodmanAuthFiles(d)
	for i := len(files) - 1; i >= 0; i-- {
		object := buildReadJSONObject(d, files[i])
		for _, key := range []string{"auths", "credHelpers"} {
			entries, ok := object[key].(map[string]any)
			if !ok {
				continue
			}
			section, _ := combined[key].(map[string]any)
			if section == nil {
				section = map[string]any{}
				combined[key] = section
			}
			for name, entry := range entries {
				section[name] = entry
			}
		}
	}
	return combined
}

func buildReadJSONObject(d Dependencies, path string) map[string]any {
	data, err := os.ReadFile(path)
	var object map[string]any
	if err == nil {
		err = json.Unmarshal(data, &object)
	}
	if err != nil {
		_, _ = fmt.Fprintf(d.Err, "Warning: ignoring unreadable %s: %s\n", output.CellText(path), output.CellText(err.Error()))
		return map[string]any{}
	}
	if object == nil {
		object = map[string]any{}
	}
	return object
}

// buildPodmanDockerConfig writes a private DOCKER_CONFIG for standalone
// buildx: the user's docker config.json, if any, with podman's registry
// credentials laid over it, so --push authenticates as after `podman login`.
// buildx keeps its builder state there too, so the builder goes away with the
// temporary directory.
func buildPodmanDockerConfig(d Dependencies, directory string) (string, error) {
	target := filepath.Join(directory, "docker-config")
	if err := os.Mkdir(target, 0o700); err != nil {
		return "", err
	}
	configured := d.Getenv("DOCKER_CONFIG")
	if configured == "" {
		configured = buildExpandHome("~/.docker")
	}
	merged := map[string]any{}
	if source := filepath.Join(configured, "config.json"); isRegularFile(source) {
		merged = buildReadJSONObject(d, source)
	}
	// The private directory has no contexts/, so a copied context name would
	// fail every buildx call that has no DOCKER_HOST.
	delete(merged, "currentContext")
	for _, host := range buildMergePodmanAuth(merged, buildPodmanAuth(d)) {
		_, _ = fmt.Fprintf(d.Err, "Warning: podman holds credentials for several namespaces of %s; docker's credentials for it are used instead.\n", output.CellText(host))
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return target, os.WriteFile(filepath.Join(target, "config.json"), data, 0o600)
}

// Docker keeps Docker Hub credentials under this key, whatever name the
// registry was logged in to with.
const buildDockerHubKey = "https://index.docker.io/v1/"

var buildDockerHubHosts = []string{"docker.io", "index.docker.io", "registry-1.docker.io"}

// buildMergePodmanAuth lays podman's registry credentials over a docker
// config. Docker consults a registry's credHelpers entry, then credsStore,
// and only then auths, so a docker helper would shadow a podman login. Each
// registry podman holds credentials for therefore gets an empty credHelpers
// entry, which sends the lookup to auths, unless podman names a helper of
// its own for it.
//
// Docker's store holds one credential per registry host, while podman may
// scope one to a namespace (quay.io/acme). A namespace login stands for its
// host only when it is podman's sole credential there; with several, docker
// could pick the wrong account, so the host is left to docker and returned.
func buildMergePodmanAuth(merged, podman map[string]any) (skipped []string) {
	section := func(key string) map[string]any {
		combined, _ := merged[key].(map[string]any)
		if combined == nil {
			combined = map[string]any{}
		}
		merged[key] = combined
		return combined
	}
	if entries, ok := podman["auths"].(map[string]any); ok && len(entries) > 0 {
		auths, helpers := section("auths"), section("credHelpers")
		byHost := map[string][]string{}
		hostLevel := map[string]bool{}
		for name := range entries {
			host, path, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(name, "https://"), "http://"), "/")
			if slices.Contains(buildDockerHubHosts, host) {
				host = "docker.io"
				// The legacy index URL names the registry, not a namespace.
				if path == "v1" || path == "v1/" {
					path = ""
				}
			}
			byHost[host] = append(byHost[host], name)
			if path == "" {
				hostLevel[host] = true
			}
		}
		hosts := slices.Sorted(maps.Keys(byHost))
		for _, host := range hosts {
			names := byHost[host]
			slices.Sort(names)
			if !hostLevel[host] && len(names) > 1 {
				skipped = append(skipped, host)
				continue
			}
			var chosen any
			for _, name := range names {
				auths[name] = entries[name]
				if chosen == nil || !strings.Contains(strings.TrimPrefix(strings.TrimPrefix(name, "https://"), "http://"), "/") {
					chosen = entries[name]
				}
			}
			helpers[host] = ""
			if host == "docker.io" {
				auths[buildDockerHubKey] = chosen
				helpers[buildDockerHubKey] = ""
			}
		}
	}
	if entries, ok := podman["credHelpers"].(map[string]any); ok && len(entries) > 0 {
		helpers := section("credHelpers")
		for name, entry := range entries {
			helpers[name] = entry
		}
	}
	return skipped
}

// buildPrepareEnvironment gives buildx its private builder state and, for
// podman, its private configuration and the socket to load images through.
func buildPrepareEnvironment(d Dependencies, engine *buildEngine, directory string) error {
	// buildx keeps builder state in BUILDX_CONFIG, so a build that is killed
	// before teardown leaves its builder in the temporary directory instead
	// of the user's buildx configuration. Later entries win over the
	// inherited ones.
	engine.env = append(os.Environ(), "BUILDX_CONFIG="+filepath.Join(directory, "buildx"))
	if engine.name != "podman" {
		return nil
	}
	config, err := buildPodmanDockerConfig(d, directory)
	if err != nil {
		return err
	}
	engine.env = append(engine.env, "DOCKER_CONFIG="+config)
	if engine.dockerHost != "" {
		engine.env = append(engine.env, "DOCKER_HOST="+engine.dockerHost)
	}
	return nil
}

func (e *buildEngine) command(args ...string) ToolCommand {
	return ToolCommand{Args: append(append([]string{}, e.buildx...), args...), Env: e.env}
}
