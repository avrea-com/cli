package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// `avr build` takes `docker build` arguments. It keeps its own flags and
// forwards the rest to a local `buildx build` that talks to buildkitd in an
// ephemeral builder VM. It creates one builder session per platform, reaches
// each VM's buildkitd through a host-key-pinned `ssh -L` forward to a private
// unix socket, joins them
// into one buildx remote builder, and removes all of it when the build exits.

var buildSizes = []string{"1-vcpu", "2-vcpu", "4-vcpu", "8-vcpu", "16-vcpu", "32-vcpu"}

const (
	// The TTL bounds mirror the API, to fail bad input with a clear message.
	buildDefaultTTL = 3600
	buildMinimumTTL = 300
	buildMaximumTTL = 8 * 3600

	// buildkitd listens on this loopback port in the builder VM. The VM
	// exposes only sshd, so the port is reached through the tunnel.
	buildkitdGuestPort = 1234

	buildPollInterval     = 3 * time.Second
	buildTunnelTimeout    = 60 * time.Second
	buildTunnelRetryDelay = 2 * time.Second
	buildTeardownTimeout  = 60 * time.Second
	buildDeleteAttempts   = 3
	buildDeleteRetryDelay = 2 * time.Second

	// sun_path holds 104 bytes on macOS and 108 on Linux and Windows, with
	// a NUL.
	buildSocketPathLimit = 100
)

func NewBuild(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var org, repo, size, ttl, engine string
	var platforms []string
	var keep bool
	var timeout int
	c := &cobra.Command{Use: "build [BUILDX_ARGS...]", Short: "Build container images remotely with docker buildx on Avrea builders.", DisableFlagParsing: true}
	f := c.Flags()
	f.StringArrayVar(&platforms, "platform", nil, "Target platforms: linux/amd64 and/or linux/arm64, comma-separated or repeated.")
	f.StringVarP(&repo, "repo", "R", "", "Repository the build belongs to (org/repo or rep-...); scopes the build cache.")
	// No -o shorthand: buildx owns it as --output.
	f.StringVar(&org, "org", "", "Organization ID or slug; defaults to the configured organization.")
	f.StringVar(&size, "size", "4-vcpu", "Builder VM size (per platform).")
	f.StringVar(&ttl, "ttl", "", "Builder session TTL, e.g. 30m or 2h. Default 1h, max 8h.")
	f.BoolVar(&keep, "keep-sessions", false, "Leave the builder sessions running after the build.")
	f.IntVar(&timeout, "provision-timeout", 300, "Seconds to wait for builder VMs to become reachable.")
	f.StringVar(&engine, "engine", "auto", "Local container tooling: auto, docker or podman.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		// Persistent flags must be present before splitting known/forwarded arguments.
		cmd.Flags().AddFlagSet(cmd.InheritedFlags())
		// What follows `--` is buildx's alone; `--` itself must reach buildx
		// too, so that a context starting with '-' is not taken as a flag.
		var literal []string
		if i := slices.Index(args, "--"); i >= 0 {
			args, literal = args[:i], args[i:]
		}
		args, err := vmSSHPassthrough(cmd, args)
		if err != nil {
			return err
		}
		args = append(args, literal...)
		if err = prepareGlobalOptions(cmd, d); err != nil {
			return err
		}
		if help, _ := cmd.Flags().GetBool("help"); help {
			return cmd.Help()
		}
		ctx := cmd.Context()
		job := &buildJob{d: d, size: size, keep: keep, timeout: timeout, ttl: buildDefaultTTL}
		if job.platforms, err = buildSplitPlatforms(d, platforms); err != nil {
			return err
		}
		parsed := parseBuildArgs(args)
		// The build uses the builder it creates; another one would silently
		// take the build elsewhere.
		if parsed.has("--builder") {
			return usageError(cmd, "--builder is managed by avr build and cannot be overridden.")
		}
		if len(job.platforms) > 1 && !parsed.hasOutput() && !parsed.evaluatesOnly() {
			return usageError(cmd, "Multi-platform builds produce a manifest list, which cannot be loaded into the local docker daemon. Add --push (or --output type=...) to the build flags.")
		}
		if err = validateChoice("size", size, buildSizes); err != nil {
			return err
		}
		if cmd.Flags().Changed("ttl") {
			if job.ttl, err = tokenDuration("--ttl", ttl, buildMinimumTTL, buildMaximumTTL); err != nil {
				return err
			}
		}
		if timeout < 1 {
			return &ExitError{2, "Error: Invalid value for '--provision-timeout': must be at least 1."}
		}
		if !cmd.Flags().Changed("engine") {
			if fromEnv := d.Getenv("AVR_BUILD_ENGINE"); fromEnv != "" {
				engine = fromEnv
			}
		}
		if err = validateChoice("engine", engine, buildEngines); err != nil {
			return err
		}
		if cmd.Flags().Changed("repo") && strings.TrimSpace(repo) == "" {
			return &ExitError{2, "Error: --repo cannot be empty."}
		}
		if job.cfg, job.api, job.org, err = organizationContext(ctx, d, org); err != nil {
			return err
		}
		job.repoOption = repo
		if !parsed.hasContext {
			// Like `podman build`, the context defaults to the current directory.
			args = append(args, ".")
			parsed = parseBuildArgs(args)
		}
		// Added flags go first so that the context stays the final argument.
		job.args = slices.Concat(parsed.dockerfileArgs(), parsed.outputArgs(), args)
		job.parsed = parseBuildArgs(job.args)
		if job.engine, err = buildResolveEngine(ctx, d, engine, job.parsed); err != nil {
			return err
		}
		return job.run(ctx)
	})
	return c
}

// resolveRepository finds the repository the build belongs to: --repo, then
// AVR_REPO, then the git remote. It only scopes the build cache, so none at
// all is fine, and one found from the git remote that the organization does
// not have is dropped with a note. Sessions take the repository as owner/name.
func (j *buildJob) resolveRepository(ctx context.Context) error {
	name, platform, detected := j.repoOption, "", false
	if name == "" && j.cfg.Repo == "" {
		if directory, elsewhere := j.contextElsewhere(); elsewhere {
			// The cache belongs to the repository being built, not to the one
			// avr happens to run in.
			remote := buildGitOrigin(ctx, j.d, directory)
			if name, platform = parseGitRemote(remote); name == "" {
				return ctx.Err()
			}
			detected = true
			if j.d.IsErrorTerminal() {
				_, _ = fmt.Fprintln(j.d.Err, j.d.errorPaint("(using repo: "+output.CellText(name)+")", "2"))
			}
		}
	}
	if name == "" {
		var err error
		if name, platform, detected, err = detectRepository(ctx, j.d, j.cfg); err != nil || name == "" {
			return err
		}
	}
	id, err := resolveRepository(ctx, j.d, j.cfg, j.api, j.org, name, platform)
	if err != nil {
		var missing *missingRepository
		if detected && errors.As(err, &missing) {
			return j.note("Note: repository %s is not connected to this org; building without a repository (no repo-scoped cache). Pass --repo to choose one.", output.CellText(name))
		}
		return err
	}
	record, err := j.api.Repository(ctx, j.org, id)
	if err != nil {
		if ctx.Err() != nil || !strings.Contains(name, "/") {
			return apiError(j.cfg, err, "get repository")
		}
		// The name resolved, so the build can still use it; only the cache's
		// default-branch read fallback is lost.
		j.warn("could not read repository %s (%s); its default branch's cache will not be read.", output.CellText(name), output.CellText(err.Error()))
		j.repo = name
		return nil
	}
	j.repo, j.defaultBranch = fieldText(record, "full_name"), fieldText(record, "default_branch")
	if j.repo == "" {
		return errors.New("unexpected API response: full_name is missing")
	}
	return nil
}

// contextElsewhere returns the local build context directory when it is
// not the working directory.
func (j *buildJob) contextElsewhere() (string, bool) {
	directory := j.parsed.contextDirectory()
	if directory == "" {
		return "", false
	}
	working, err := os.Getwd()
	if err != nil {
		return "", false
	}
	return directory, !buildSamePath(directory, working)
}

// buildResolvePath returns path made absolute with symlinks resolved, or as
// far as that succeeds.
func buildResolvePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path
}

func buildSamePath(a, b string) bool {
	return buildResolvePath(a) == buildResolvePath(b)
}

// buildWithin reports whether path is directory or inside it.
func buildWithin(path, directory string) bool {
	relative, err := filepath.Rel(buildResolvePath(directory), buildResolvePath(path))
	return err == nil && filepath.IsLocal(relative)
}

func buildGitOrigin(ctx context.Context, d Dependencies, directory string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := d.RunTool(ctx, ToolCommand{Args: []string{"git", "-C", directory, "remote", "get-url", "origin"}})
	if err != nil || result.Code != 0 {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// buildSSH is how to reach one session's sshd with its host key pinned.
type buildSSH struct {
	host, user string
	port       int
	known      string
}

type buildTunnel struct {
	*vmTunnel
	platform string
	// endpoint is buildkitd's local end as a buildx remote address.
	endpoint string
}

type buildJob struct {
	d            Dependencies
	cfg          config.Config
	api          *client.Client
	engine       *buildEngine
	org, size    string
	repoOption   string
	platforms    []string
	args         []string
	parsed       buildArgs
	ttl, timeout int
	keep         bool

	// The repository as owner/name, and its default branch, once resolved.
	repo, defaultBranch string

	// What the build has made so far, for teardown.
	sessions []string
	tunnels  []*buildTunnel
	builder  string
	// How to reach each session that published an SSH endpoint, by id.
	ssh map[string]buildSSH
	// Where each session's buildkitd is forwarded to, by creation order.
	sockets  []string
	identity string
}

func (j *buildJob) note(format string, values ...any) error {
	_, err := fmt.Fprintf(j.d.Err, format+"\n", values...)
	return err
}

func (j *buildJob) warn(format string, values ...any) {
	_, _ = fmt.Fprintf(j.d.Err, "Warning: "+format+"\n", values...)
}

func (j *buildJob) sessionPath(id string) string {
	return builderBase(j.org) + "/" + url.PathEscape(id)
}

// run builds and then removes whatever the build made, on every way out.
func (j *buildJob) run(parent context.Context) (err error) {
	// The temporary directory holds the session key and registry
	// credentials, which must not be sent to the builder with the context.
	for _, directory := range j.parsed.localDirectories() {
		if buildWithin(os.TempDir(), directory) {
			return &ExitError{1, "Error: the temporary directory " + output.CellText(os.TempDir()) + " is inside the build context " + output.CellText(directory) + ", which would send the build's credentials to the builder; set TMPDIR outside the build context."}
		}
	}
	// SIGTERM, and SIGHUP from a closed terminal, must unwind through
	// teardown instead of leaving the sessions to their TTL.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var caught atomic.Int32
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case received := <-signals:
			if number, ok := received.(syscall.Signal); ok {
				caught.Store(int32(number))
			}
			cancel()
		case <-done:
		}
	}()
	defer func() {
		signal.Stop(signals)
		close(done)
	}()
	directory, err := os.MkdirTemp("", "avr-build-")
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), buildTeardownTimeout)
		defer cancel()
		j.teardown(cleanup, directory)
		if number := caught.Load(); number != 0 && parent.Err() == nil {
			// 128 + the signal, the conventional exit code for it.
			err = &ExitError{Code: 128 + int(number)}
		}
	}()
	// A path too long for a socket must fail before any session exists.
	for i := range j.platforms {
		socket := filepath.Join(directory, fmt.Sprintf("buildkitd-%d.sock", i))
		if len(socket) > buildSocketPathLimit {
			return &ExitError{1, "Error: the temporary directory path is too long for a unix socket (" + output.CellText(socket) + "); set TMPDIR to a shorter directory."}
		}
		j.sockets = append(j.sockets, socket)
	}
	code, err := j.build(ctx, directory)
	if err != nil {
		return err
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}

func (j *buildJob) build(ctx context.Context, directory string) (int, error) {
	if err := buildPrepareEnvironment(j.d, j.engine, directory); err != nil {
		return 0, err
	}
	identity, public, err := buildGenerateKey(directory)
	if err != nil {
		return 0, err
	}
	j.identity = identity
	if err = j.resolveRepository(ctx); err != nil {
		return 0, err
	}
	for _, platform := range j.platforms {
		if err = j.createSession(ctx, platform, public); err != nil {
			return 0, err
		}
	}
	ready, err := j.waitForSessions(ctx, directory)
	if err != nil {
		return 0, err
	}
	for i, record := range ready {
		if err = j.openTunnel(ctx, record, i); err != nil {
			return 0, err
		}
	}
	// Named before it exists, so that a partly created builder is removed.
	j.builder = "avr-" + j.sessions[0]
	if err = j.createBuilder(ctx); err != nil {
		return 0, err
	}
	args := j.args
	// Only a session with a repository has a cache scope on the proxy.
	if j.repo != "" {
		args = slices.Concat(buildCacheArgs(ctx, j.d, j.parsed, j.platforms, j.defaultBranch), j.args)
	}
	return j.runBuild(ctx, args)
}

// createSession requests one builder session.
func (j *buildJob) createSession(ctx context.Context, platform, public string) error {
	body := map[string]any{"platform": platform, "size": j.size, "ssh_public_keys": []string{public}, "ttl_seconds": j.ttl}
	if j.repo != "" {
		body["repo"] = j.repo
	}
	record, err := j.api.CreateBuilderSession(ctx, builderBase(j.org), body)
	if err != nil {
		if he, ok := errors.AsType[*client.HTTPError](err); ok && he.Status == 409 {
			// Quota: active sessions are limited per organization and member.
			return withHint(apiError(j.cfg, err, "create builder session"), "you may be at the builder-session quota; `avr builder list` shows active sessions and `avr builder stop <id>` reclaims one.")
		}
		return apiError(j.cfg, err, "create builder session")
	}
	id := fieldText(record, "builder_session_id")
	if id == "" {
		return errors.New("unexpected API response: builder_session_id is missing")
	}
	j.sessions = append(j.sessions, id)
	return j.note("Created builder session %s (%s, %s)", output.CellText(id), platform, j.size)
}

// buildSessionDisposition classifies a polled session as "ready", "failed" or
// "pending". RUNNING alone is not ready: endpoints must be present, and
// RUNNING with state_reason=endpoint_unavailable means they never will be.
func buildSessionDisposition(record map[string]any) (string, string) {
	id := output.CellText(fieldText(record, "builder_session_id"))
	platform := output.CellText(fieldText(record, "platform"))
	reason := output.CellText(fieldText(record, "state_reason"))
	detail := ""
	if reason != "" {
		detail = ": " + reason
	}
	switch strings.ToUpper(fieldText(record, "state")) {
	case "RUNNING":
		if endpoints, _ := record["endpoints"].(map[string]any); len(endpoints) > 0 {
			return "ready", ""
		}
		if reason == "endpoint_unavailable" {
			return "failed", fmt.Sprintf("builder session %s (%s) published no usable SSH endpoint; it will not become reachable. Try again.", id, platform)
		}
	case "FAILED":
		if reason == "no_capacity" {
			return "failed", fmt.Sprintf("no builder capacity for %s right now; try again shortly.", platform)
		}
		return "failed", fmt.Sprintf("builder session %s (%s) failed to start%s.", id, platform, detail)
	case "STOPPING", "STOPPED":
		return "failed", fmt.Sprintf("builder session %s (%s) stopped unexpectedly%s.", id, platform, detail)
	}
	return "pending", ""
}

// waitForSessions polls every session in turn until all are RUNNING with
// endpoints, and returns them in creation order. The first definite failure
// ends the wait; transient poll failures are retried until the deadline.
func (j *buildJob) waitForSessions(ctx context.Context, directory string) ([]map[string]any, error) {
	if err := j.note("Waiting up to %ds for %d builder session(s)...", j.timeout, len(j.sessions)); err != nil {
		return nil, err
	}
	deadline := j.d.Now().Add(time.Duration(j.timeout) * time.Second)
	notReady := &ExitError{1, fmt.Sprintf("Error: builder session(s) not ready after %ds; raise --provision-timeout or try again (`avr builder list` shows session state).", j.timeout)}
	// The deadline also bounds each request, so that a slow answer cannot
	// report a session ready after it.
	polling, cancel := context.WithTimeout(ctx, time.Duration(j.timeout)*time.Second)
	defer cancel()
	ready := map[string]map[string]any{}
	last := map[string]string{}
	for {
		delay := buildPollInterval
		for _, id := range j.sessions {
			if ready[id] != nil {
				continue
			}
			record, err := j.api.BuilderSession(polling, j.sessionPath(id))
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if polling.Err() != nil {
					return nil, notReady
				}
				retry, err := pollFailure(j.cfg, err, "check builder session status")
				if err != nil {
					return nil, err
				}
				delay = max(delay, retry)
				continue
			}
			state := strings.ToUpper(fieldText(record, "state"))
			if shown, seen := last[id]; !seen || shown != state {
				if err = j.note("  %s %s", output.CellText(id), output.CellText(stateLabel(state))); err != nil {
					return nil, err
				}
				last[id] = state
			}
			switch disposition, message := buildSessionDisposition(record); disposition {
			case "failed":
				return nil, &ExitError{1, "Error: " + message}
			case "ready":
				if err = j.recordSSH(record, directory, slices.Index(j.sessions, id)); err != nil {
					return nil, err
				}
				ready[id] = record
			}
		}
		if len(ready) == len(j.sessions) {
			records := make([]map[string]any, 0, len(j.sessions))
			for _, id := range j.sessions {
				records = append(records, ready[id])
			}
			return records, nil
		}
		remaining := deadline.Sub(j.d.Now())
		if remaining <= 0 {
			return nil, notReady
		}
		if err := j.d.Wait(ctx, min(delay, remaining)); err != nil {
			return nil, err
		}
	}
}

// recordSSH pins a ready session's SSH host key in a private known_hosts,
// so that the tunnel and, with --keep-sessions, the user's own ssh trust only
// that key. The tunnel carries the build context, secrets and registry
// credentials, so a session without a host key is refused rather than
// trusted on first use.
func (j *buildJob) recordSSH(record map[string]any, directory string, index int) error {
	id, platform := fieldText(record, "builder_session_id"), fieldText(record, "platform")
	endpoints, _ := record["endpoints"].(map[string]any)
	endpoint, _ := endpoints["ssh"].(map[string]any)
	host, user, port, err := vmSSHAddress(endpoint)
	if err != nil {
		return err
	}
	if fieldText(endpoint, "host_key") == "" {
		return &ExitError{1, fmt.Sprintf("Error: builder session %s (%s) published no SSH host key; refusing to send the build over an unverified connection. Try again.", output.CellText(id), output.CellText(platform))}
	}
	line, err := vmKnownHostsLine(fieldText(endpoint, "host_key"), host, port)
	if err != nil {
		return err
	}
	// A private known_hosts keeps builder keys out of the user's, where a
	// later builder at the same address would trip over this one's key.
	known := filepath.Join(directory, fmt.Sprintf("known_hosts-%d", index))
	if err = os.WriteFile(known, []byte(line), 0o600); err != nil {
		return err
	}
	if j.ssh == nil {
		j.ssh = map[string]buildSSH{}
	}
	j.ssh[id] = buildSSH{host, user, port, known}
	return nil
}

// openTunnel forwards buildkitd in the builder VM to a unix socket in the
// build's private directory, through the session's SSH endpoint with its host
// key pinned, so only the user can reach the local end. ssh is retried while
// the VM's sshd may still be starting.
func (j *buildJob) openTunnel(ctx context.Context, record map[string]any, index int) error {
	id, platform := fieldText(record, "builder_session_id"), fieldText(record, "platform")
	login := j.ssh[id]
	forward := vmForward{guest: buildkitdGuestPort, socket: j.sockets[index]}
	remote := "unix://" + filepath.ToSlash(forward.socket)
	forwards := []vmForward{forward}
	argv := vmTunnelArgv(login.host, login.user, login.port, forwards, j.identity, login.known)
	// Never prompt: standard input stays with buildx, which may read the
	// context from it. The pinned key is the only trust: no system
	// known_hosts, no user ssh config, and no shared connection that skipped
	// this host key check.
	argv = slices.Insert(argv, 1, "-F", "none")
	argv = slices.Insert(argv, len(argv)-1, "-o", "BatchMode=yes", "-o", "GlobalKnownHostsFile=none", "-o", "ControlMaster=no", "-o", "ControlPath=none")
	detached := j.d
	detached.In = nil
	deadline := j.d.Now().Add(buildTunnelTimeout)
	for {
		// The tunnel outlives a cancelled build: removing the buildx builder
		// comes first in teardown.
		tunnel, ready, err := vmStartTunnel(ctx, context.WithoutCancel(ctx), detached, argv, forwards, deadline.Sub(j.d.Now()))
		if err != nil {
			return err
		}
		if ready {
			j.tunnels = append(j.tunnels, &buildTunnel{vmTunnel: tunnel, platform: platform, endpoint: remote})
			return j.note("Builder ready: %s (%s)", output.CellText(id), output.CellText(platform))
		}
		remaining := deadline.Sub(j.d.Now())
		if remaining <= 0 {
			return &ExitError{1, "Error: the tunnel to the " + output.CellText(platform) + " builder did not come up; the VM may be unreachable. Try again (`avr builder list` shows session state)."}
		}
		_ = os.Remove(forward.socket)
		if err = j.d.Wait(ctx, min(buildTunnelRetryDelay, remaining)); err != nil {
			return err
		}
	}
}

func buildToolDetail(result ToolResult) string {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	return output.Text(detail)
}

// createBuilder joins every tunnel into one buildx remote builder. Each node
// is pinned to its platform, so buildx runs each architecture natively.
func (j *buildJob) createBuilder(ctx context.Context) error {
	for i, tunnel := range j.tunnels {
		args := []string{"create", "--name", j.builder, "--driver", "remote"}
		if i > 0 {
			args = append(args, "--append")
		}
		args = append(args, "--platform", tunnel.platform, tunnel.endpoint)
		result, err := j.d.RunTool(ctx, j.engine.command(args...))
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return buildToolFailure("buildx create failed: %s", err)
		}
		if result.Code != 0 {
			return buildToolFailure("buildx create failed: %s", buildToolDetail(result))
		}
	}
	return nil
}

// runBuild runs the forwarded build on the builder and returns its exit code.
// Progress streams as it does for `docker build`. Ctrl-C belongs to buildx
// while it runs, so BuildKit cancels the build itself; teardown follows.
func (j *buildJob) runBuild(ctx context.Context, args []string) (int, error) {
	command := j.engine.command(slices.Concat([]string{"build", "--builder", j.builder, "--platform", strings.Join(j.platforms, ",")}, args)...)
	command.Stream = true
	restore := sshOwnsInterrupt(ctx)
	defer restore()
	result, err := j.d.RunTool(ctx, command)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err != nil {
		return 0, buildToolFailure("failed to run buildx: %s", err)
	}
	return result.Code, nil
}

// teardown removes what the build made, in dependency order: the buildx
// builder, the tunnels, the sessions, then the key. Each step only warns, so
// that one failure does not leave the rest behind, and the sessions get their
// own time budget so that a slow buildx cannot keep them running.
func (j *buildJob) teardown(parent context.Context, directory string) {
	if j.builder != "" {
		ctx, cancel := context.WithTimeout(parent, buildTeardownTimeout/2)
		result, err := j.d.RunTool(ctx, j.engine.command("rm", j.builder))
		cancel()
		if err != nil {
			j.warn("failed to remove buildx builder %s: %s", output.CellText(j.builder), output.CellText(err.Error()))
		} else if result.Code != 0 {
			j.warn("failed to remove buildx builder %s: %s", output.CellText(j.builder), buildToolDetail(result))
		}
	}
	for _, tunnel := range j.tunnels {
		tunnel.close()
	}
	// --keep-sessions keeps the sessions one can log in to; one that never
	// published an SSH endpoint would only hold its VM and quota.
	kept, logins, stop := []string{}, []string{}, []string{}
	for _, id := range j.sessions {
		login, reachable := j.ssh[id]
		if !j.keep || !reachable {
			stop = append(stop, id)
			continue
		}
		kept = append(kept, id)
		logins = append(logins, id+": "+vmShellJoin(vmSSHArgv(login.host, login.user, login.port, j.identity, login.known, nil)))
	}
	if j.keep && len(stop) > 0 {
		_ = j.note("Stopping builder session(s) %s despite --keep-sessions: they published no SSH endpoint.", output.CellText(strings.Join(stop, ", ")))
	}
	ctx, cancel := context.WithTimeout(parent, buildTeardownTimeout)
	for _, id := range stop {
		j.deleteSession(ctx, id)
	}
	cancel()
	if len(kept) > 0 {
		_ = j.note("Keeping builder session(s) %s running (--keep-sessions); stop with `avr builder stop <id>` or let the TTL reclaim them.", output.CellText(strings.Join(kept, ", ")))
		// The session key is the only one the VMs accept, so it stays.
		_ = j.note("Reach them with:\n  %s\nThen remove %s.", output.CellText(strings.Join(logins, "\n  ")), output.CellText(directory))
		for _, name := range []string{"buildx", "docker-config"} {
			_ = os.RemoveAll(filepath.Join(directory, name))
		}
		for _, socket := range j.sockets {
			_ = os.Remove(socket)
		}
		return
	}
	_ = os.RemoveAll(directory)
}

// deleteSession stops one session, retrying transient failures: a session
// left running holds its VM and a quota slot until the TTL. A rate limit's
// Retry-After is honored as long as the teardown budget allows.
func (j *buildJob) deleteSession(ctx context.Context, id string) {
	for attempt := 1; ; attempt++ {
		_, err := j.api.DeleteBuilderSession(ctx, j.sessionPath(id))
		if err == nil {
			return
		}
		he, isHTTP := errors.AsType[*client.HTTPError](err)
		var delay time.Duration
		var permanent error
		if isHTTP && he.Status == 404 {
			// A 404 may also mean the session's node could not take the stop.
			// Only a session that is gone or stopping is done with.
			if j.sessionStopped(ctx, id) {
				return
			}
			delay, err = buildDeleteRetryDelay, errors.New("the stop request was answered 404 while the session is still active")
		} else {
			delay, permanent = pollFailure(j.cfg, err, "stop builder session")
		}
		if permanent == nil && attempt < buildDeleteAttempts && ctx.Err() == nil {
			deadline, bounded := ctx.Deadline()
			if !bounded || time.Until(deadline) > delay {
				if j.d.Wait(ctx, delay) == nil {
					continue
				}
			}
		}
		reclaim := ""
		if isHTTP && (he.Status == 409 || he.Status == 404) {
			reclaim = " (its TTL will reclaim it)"
		}
		j.warn("failed to stop builder session %s%s: %s", output.CellText(id), reclaim, output.CellText(err.Error()))
		return
	}
}

// sessionStopped reports a session that no longer exists or is stopping.
func (j *buildJob) sessionStopped(ctx context.Context, id string) bool {
	record, err := j.api.BuilderSession(ctx, j.sessionPath(id))
	if err != nil {
		he, isHTTP := errors.AsType[*client.HTTPError](err)
		return isHTTP && he.Status == 404
	}
	switch strings.ToUpper(fieldText(record, "state")) {
	case "STOPPING", "STOPPED":
		return true
	}
	return false
}
