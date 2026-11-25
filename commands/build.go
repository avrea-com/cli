package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
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
	buildTunnelAttempts   = 5
	buildTunnelRetryDelay = 2 * time.Second
	buildTeardownTimeout  = 60 * time.Second
	buildDeleteAttempts   = 3
	buildDeleteRetryDelay = 2 * time.Second

	// sun_path holds 104 bytes on macOS and 108 on Linux, with a NUL.
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
		args, err := vmSSHPassthrough(cmd, args)
		if err != nil {
			return err
		}
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
		if len(job.platforms) > 1 && !parsed.hasOutput() {
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
	// How to reach each session with ssh, for --keep-sessions.
	logins []string
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
	// SIGTERM must unwind through teardown instead of leaving the sessions to
	// their TTL.
	ctx, stop := signal.NotifyContext(parent, syscall.SIGTERM)
	defer stop()
	directory, err := os.MkdirTemp("", "avr-build-")
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), buildTeardownTimeout)
		defer cancel()
		j.teardown(cleanup, directory)
		if ctx.Err() != nil && parent.Err() == nil {
			// 128 + SIGTERM, the conventional exit code for that signal.
			err = &ExitError{Code: 143}
		}
	}()
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
	if err = j.resolveRepository(ctx); err != nil {
		return 0, err
	}
	for _, platform := range j.platforms {
		if err = j.createSession(ctx, platform, public); err != nil {
			return 0, err
		}
	}
	ready, err := j.waitForSessions(ctx)
	if err != nil {
		return 0, err
	}
	for i, record := range ready {
		if err = j.openTunnel(ctx, record, identity, directory, i); err != nil {
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
func (j *buildJob) waitForSessions(ctx context.Context) ([]map[string]any, error) {
	if err := j.note("Waiting up to %ds for %d builder session(s)...", j.timeout, len(j.sessions)); err != nil {
		return nil, err
	}
	deadline := j.d.Now().Add(time.Duration(j.timeout) * time.Second)
	ready := map[string]map[string]any{}
	last := map[string]string{}
	for {
		delay := buildPollInterval
		for _, id := range j.sessions {
			if ready[id] != nil {
				continue
			}
			record, err := j.api.BuilderSession(ctx, j.sessionPath(id))
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
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
			return nil, &ExitError{1, fmt.Sprintf("Error: builder session(s) not ready after %ds; raise --provision-timeout or try again (`avr builder list` shows session state).", j.timeout)}
		}
		if err := j.d.Wait(ctx, min(delay, remaining)); err != nil {
			return nil, err
		}
	}
}

// openTunnel forwards buildkitd in the builder VM to a unix socket in the
// build's private directory, through the session's SSH endpoint with its host
// key pinned. The tunnel carries the build context, secrets and registry
// credentials, so a session without a host key is refused rather than
// trusted on first use, and only the user can reach the local end. ssh is
// retried while the VM's sshd may still be starting.
func (j *buildJob) openTunnel(ctx context.Context, record map[string]any, identity, directory string, index int) error {
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
	forward := vmForward{guest: buildkitdGuestPort}
	var remote string
	if runtime.GOOS == "windows" {
		// No unix socket forwarding: fall back to a loopback port.
		if forward.local, err = vmPickPort(ctx, j.d); err != nil {
			return err
		}
		remote = "tcp://127.0.0.1:" + strconv.Itoa(forward.local)
	} else {
		forward.socket = filepath.Join(directory, fmt.Sprintf("buildkitd-%d.sock", index))
		if len(forward.socket) > buildSocketPathLimit {
			return &ExitError{1, "Error: the temporary directory path is too long for a unix socket (" + output.CellText(forward.socket) + "); set TMPDIR to a shorter directory."}
		}
		remote = "unix://" + forward.socket
	}
	forwards := []vmForward{forward}
	argv := vmTunnelArgv(host, user, port, forwards, identity, known)
	// Never prompt: standard input stays with buildx, which may read the
	// context from it.
	argv = slices.Insert(argv, len(argv)-1, "-o", "BatchMode=yes")
	detached := j.d
	detached.In = nil
	deadline := j.d.Now().Add(buildTunnelTimeout)
	for attempt := 1; ; attempt++ {
		// The tunnel outlives a cancelled build: removing the buildx builder
		// comes first in teardown.
		tunnel, ready, err := vmStartTunnel(ctx, context.WithoutCancel(ctx), detached, argv, forwards, deadline.Sub(j.d.Now()))
		if err != nil {
			return err
		}
		if ready {
			j.tunnels = append(j.tunnels, &buildTunnel{vmTunnel: tunnel, platform: platform, endpoint: remote})
			j.logins = append(j.logins, id+": "+vmShellJoin(vmSSHArgv(host, user, port, identity, known, nil)))
			return j.note("Builder ready: %s (%s)", output.CellText(id), output.CellText(platform))
		}
		remaining := deadline.Sub(j.d.Now())
		if attempt == buildTunnelAttempts || remaining <= 0 {
			return &ExitError{1, "Error: the tunnel to the " + output.CellText(platform) + " builder did not come up; the VM may be unreachable. Try again (`avr builder list` shows session state)."}
		}
		if forward.socket != "" {
			_ = os.Remove(forward.socket)
		}
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
	if j.keep && len(j.sessions) > 0 {
		_ = j.note("Keeping builder session(s) %s running (--keep-sessions); stop with `avr builder stop <id>` or let the TTL reclaim them.", output.CellText(strings.Join(j.sessions, ", ")))
		if len(j.logins) > 0 {
			// The session key is the only one the VMs accept, so it stays.
			_ = j.note("Reach them with:\n  %s\nThen remove %s.", output.CellText(strings.Join(j.logins, "\n  ")), output.CellText(directory))
			for _, name := range []string{"buildx", "docker-config"} {
				_ = os.RemoveAll(filepath.Join(directory, name))
			}
			return
		}
	} else {
		ctx, cancel := context.WithTimeout(parent, buildTeardownTimeout)
		for _, id := range j.sessions {
			j.deleteSession(ctx, id)
		}
		cancel()
	}
	_ = os.RemoveAll(directory)
}

// deleteSession stops one session, retrying transient failures: a session
// left running holds its VM and a quota slot until the TTL.
func (j *buildJob) deleteSession(ctx context.Context, id string) {
	for attempt := 1; ; attempt++ {
		_, err := j.api.DeleteBuilderSession(ctx, j.sessionPath(id))
		if err == nil {
			return
		}
		he, isHTTP := errors.AsType[*client.HTTPError](err)
		if isHTTP && he.Status == 404 {
			return // Already gone.
		}
		delay, permanent := pollFailure(j.cfg, err, "stop builder session")
		if permanent == nil && attempt < buildDeleteAttempts && ctx.Err() == nil {
			if j.d.Wait(ctx, min(delay, buildDeleteRetryDelay)) == nil {
				continue
			}
		}
		reclaim := ""
		if isHTTP && he.Status == 409 {
			reclaim = " (its TTL will reclaim it)"
		}
		j.warn("failed to stop builder session %s%s: %s", output.CellText(id), reclaim, output.CellText(err.Error()))
		return
	}
}
