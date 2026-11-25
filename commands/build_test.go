package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

// buildFixture runs `avr build` against a scripted API, scripted local tools
// and a stand-in ssh that only has to stay alive.
type buildFixture struct {
	t           *testing.T
	d           Dependencies
	out, stderr *bytes.Buffer
	sshArgs     string
	directory   string

	mu       sync.Mutex
	requests []string
	bodies   []map[string]any
	tools    []ToolCommand
	polls    map[string]int
	created  int

	programs  map[string]string
	create    func(body map[string]any) (int, string)
	resolve   func(name string) (int, string)
	poll      func(id string, attempt int) (int, http.Header, string)
	tool      func(command ToolCommand) (ToolResult, error, bool)
	buildCode int
	// deadPort makes ssh exit at once and leaves its forwarded port closed.
	deadPort bool
}

// buildOutput serializes writes. A bytes.Buffer handed to a child process is
// filled through ReadFrom, which drops what the command writes meanwhile.
type buildOutput struct {
	mu     sync.Mutex
	buffer *bytes.Buffer
}

func (o *buildOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.Write(data)
}

const buildRepositoryID = "rep-019a0000000070008000000000000002"

const buildReadySession = `{"data":{"builder_session_id":%q,"platform":%q,"state":"RUNNING","endpoints":{"ssh":{"external_ip":"203.0.113.5","external_port":2222,"username":"runner","host_key":"ssh-ed25519 AAAAfixture"}}}}`

func buildJSONResponse(r *http.Request, status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func newBuildFixture(t *testing.T) *buildFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable stub")
	}
	f := &buildFixture{t: t, polls: map[string]int{}, programs: map[string]string{"docker": "/fixture/docker"}}
	f.d, f.out, f.stderr = testDeps(t)
	f.d.Out, f.d.Err = &buildOutput{buffer: f.out}, &buildOutput{buffer: f.stderr}
	directory := t.TempDir()
	f.directory = directory
	f.sshArgs = filepath.Join(directory, "argv")
	t.Chdir(t.TempDir())
	f.d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	f.d.GitRemote = func(context.Context) (string, error) { return "git@github.com:acme/widgets.git", nil }
	// Tunnel retries move the clock on, so a tunnel that never comes up
	// exhausts its deadline without waiting for it.
	var skew atomic.Int64
	f.d.Now = func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }
	f.d.Wait = func(ctx context.Context, delay time.Duration) error {
		if delay == buildTunnelRetryDelay {
			skew.Add(int64(delay))
		}
		return ctx.Err()
	}
	f.d.LookPath = func(name string) (string, error) {
		if path := f.programs[name]; path != "" {
			return path, nil
		}
		return "", os.ErrNotExist
	}
	f.d.RunTool = func(_ context.Context, command ToolCommand) (ToolResult, error) {
		f.mu.Lock()
		f.tools = append(f.tools, command)
		f.mu.Unlock()
		if f.tool != nil {
			if result, err, handled := f.tool(command); handled {
				return result, err
			}
		}
		switch line := strings.Join(command.Args, " "); {
		case strings.HasSuffix(line, "version"):
			return ToolResult{Stdout: "github.com/docker/buildx v0.20.0"}, nil
		case command.Args[0] == "git", command.Args[0] == "podman":
			return ToolResult{Code: 1}, nil
		case slices.Contains(command.Args, "build"):
			return ToolResult{Code: f.buildCode}, nil
		}
		return ToolResult{}, nil
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	f.d.Listen = func(ctx context.Context, network, address string) (net.Listener, error) {
		if f.deadPort {
			return (&net.ListenConfig{}).Listen(ctx, network, address)
		}
		return &vmPortListener{port: listener.Addr().(*net.TCPAddr).Port}, nil
	}
	// The stand-in ssh is this test binary, which serves the forwarded
	// unix socket as ssh would; see TestBuildSSHStub.
	script := "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done >> '" + f.sshArgs + "'\nif [ -e '" + directory + "/dead' ]; then exit 1; fi\ncase \"$*\" in *buildkitd-1.sock*) if [ -e '" + directory + "/dead-1' ]; then exit 1; fi;; esac\nexport AVR_BUILD_SSH_STUB=1\nexec '" + os.Args[0] + "' -test.run='^TestBuildSSHStub$' -- \"$@\"\n"
	if err = os.WriteFile(filepath.Join(directory, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	f.d.HTTP = &http.Client{Transport: watchTransportFunc(f.serve)}
	t.Setenv("PATH", directory)
	return f
}

// TestBuildSSHStub is the stand-in ssh of buildFixture: it listens on each
// `-L` unix socket and stays alive until it is stopped.
func TestBuildSSHStub(t *testing.T) {
	if os.Getenv("AVR_BUILD_SSH_STUB") != "1" {
		t.Skip("run as ssh by buildFixture")
	}
	args := os.Args
	for i, arg := range args {
		if arg != "-L" || i+1 == len(args) {
			continue
		}
		bind, _, _ := strings.Cut(args[i+1], ":")
		if !strings.HasPrefix(bind, "/") {
			continue
		}
		if _, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", bind); err != nil {
			os.Exit(255)
		}
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func (f *buildFixture) serve(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	base := "/orgs/" + inspectionOrg + "/builder-sessions"
	switch {
	case r.Method == http.MethodPost && r.URL.Path == base:
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
		}
		f.bodies = append(f.bodies, body)
		if f.create != nil {
			if status, text := f.create(body); status != 0 {
				return buildJSONResponse(r, status, nil, text), nil
			}
		}
		f.created++
		return buildJSONResponse(r, 201, nil, fmt.Sprintf(`{"data":{"builder_session_id":"bs-%d","platform":%q,"state":"PENDING"}}`, f.created, body["platform"])), nil
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, base+"/"):
		id := strings.TrimPrefix(r.URL.Path, base+"/")
		f.polls[id]++
		if f.poll != nil {
			if status, header, text := f.poll(id, f.polls[id]); status != 0 {
				return buildJSONResponse(r, status, header, text), nil
			}
		}
		platform := "linux/amd64"
		if id == "bs-2" {
			platform = "linux/arm64"
		}
		return buildJSONResponse(r, 200, nil, fmt.Sprintf(buildReadySession, id, platform)), nil
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, base+"/"):
		return buildJSONResponse(r, 200, nil, `{"data":{"state":"STOPPING"}}`), nil
	case r.Method == http.MethodGet && r.URL.Path == "/orgs/"+inspectionOrg+"/repos/resolve":
		if f.resolve != nil {
			if status, text := f.resolve(r.URL.Query().Get("name")); status != 0 {
				return buildJSONResponse(r, status, nil, text), nil
			}
		}
		return buildJSONResponse(r, 200, nil, `{"data":{"repository_id":"`+buildRepositoryID+`","full_name":"acme/widgets"}}`), nil
	case r.Method == http.MethodGet && r.URL.Path == "/orgs/"+inspectionOrg+"/repos/"+buildRepositoryID:
		return buildJSONResponse(r, 200, nil, `{"data":{"repository_id":"`+buildRepositoryID+`","full_name":"acme/widgets","default_branch":"main"}}`), nil
	}
	f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	return buildJSONResponse(r, 500, nil, `{}`), nil
}

func (f *buildFixture) run(args ...string) int {
	f.t.Helper()
	if f.deadPort {
		if err := os.WriteFile(filepath.Join(f.directory, "dead"), nil, 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
	return Execute(f.t.Context(), append([]string{"build"}, args...), f.d)
}

// buildx returns the recorded buildx invocations of one subcommand.
func (f *buildFixture) buildx(subcommand string) [][]string {
	found := [][]string{}
	for _, command := range f.tools {
		if len(command.Args) > 2 && command.Args[0] == "docker" && command.Args[2] == subcommand {
			found = append(found, command.Args)
		}
	}
	return found
}

func (f *buildFixture) count(request string) int {
	total := 0
	for _, seen := range f.requests {
		if seen == request {
			total++
		}
	}
	return total
}

func (f *buildFixture) deleted(id string) bool {
	return f.count("DELETE /orgs/"+inspectionOrg+"/builder-sessions/"+id) == 1
}

func TestBuildRunsBuildxAgainstATunnelledBuilder(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("--platform", "linux/amd64", "--size", "8-vcpu", "--ttl", "30m", "-t", "img", "--build-arg", "A=1", "."); code != 0 {
		t.Fatal(code, f.stderr)
	}
	want := map[string]any{"platform": "linux/amd64", "size": "8-vcpu", "ttl_seconds": float64(1800), "repo": "acme/widgets"}
	body := f.bodies[0]
	keys, _ := body["ssh_public_keys"].([]any)
	if len(keys) != 1 || !strings.HasPrefix(fmt.Sprint(keys[0]), "ssh-ed25519 ") {
		t.Fatal(body)
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("%s: %v != %v", key, body[key], value)
		}
	}
	create, build, remove := f.buildx("create"), f.buildx("build"), f.buildx("rm")
	if len(create) != 1 || len(build) != 1 || len(remove) != 1 {
		t.Fatal(f.tools)
	}
	if got := strings.Join(create[0][:9], " "); got != "docker buildx create --name avr-bs-1 --driver remote --platform linux/amd64" || !strings.HasPrefix(create[0][9], "unix:///") {
		t.Error(create[0])
	}
	if got := strings.Join(build[0][:7], " "); got != "docker buildx build --builder avr-bs-1 --platform linux/amd64" {
		t.Error(got)
	}
	// Cache defaults lead, then --load, then the user's arguments with the
	// context last.
	rest := build[0][7:]
	if rest[0] != "--cache-from" || rest[2] != "--cache-to" || strings.Join(rest[4:], " ") != "--load -t img --build-arg A=1 ." {
		t.Error(rest)
	}
	if !strings.Contains(rest[1], "scope=avr-dockerfile-linux-amd64") || !strings.HasSuffix(rest[3], ",mode=max,ignore-error=true") || !strings.Contains(rest[1], "token="+buildCacheToken("main")+",") {
		t.Error(rest[1], rest[3])
	}
	if strings.Join(remove[0], " ") != "docker buildx rm avr-bs-1" || !f.deleted("bs-1") {
		t.Error(remove, f.requests)
	}
	ssh, err := os.ReadFile(f.sshArgs)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSpace(string(ssh)), "\n")
	socket := strings.TrimPrefix(create[0][9], "unix://")
	if !slices.Contains(argv, socket+":localhost:1234") || !slices.Contains(argv, "StrictHostKeyChecking=yes") || !slices.Contains(argv, "BatchMode=yes") || !slices.Contains(argv, "IdentitiesOnly=yes") || argv[len(argv)-1] != "runner@203.0.113.5" {
		t.Error(argv)
	}
	if _, err = os.Stat(socket); !os.IsNotExist(err) {
		t.Error("left behind:", socket, err)
	}
	// The pinned host key and the session key do not outlive the build.
	for i, arg := range argv {
		path := strings.TrimPrefix(arg, "UserKnownHostsFile=")
		if arg == "-i" {
			path = argv[i+1]
		} else if path == arg {
			continue
		}
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Error("left behind:", path, err)
		}
	}
	for _, text := range []string{"Using docker (docker)", "Created builder session bs-1 (linux/amd64, 8-vcpu)", "  bs-1 RUNNING", "Builder ready: bs-1 (linux/amd64)"} {
		if !strings.Contains(f.stderr.String(), text) {
			t.Errorf("missing %q in %s", text, f.stderr)
		}
	}
}

func TestBuildDefaultsToTheNativePlatform(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("."); code != 0 {
		t.Fatal(code, f.stderr)
	}
	want, _ := buildNativePlatform(runtime.GOARCH)
	if f.bodies[0]["platform"] != want || f.bodies[0]["size"] != "4-vcpu" || f.bodies[0]["ttl_seconds"] != float64(3600) {
		t.Fatal(f.bodies[0])
	}
	for architecture, platform := range map[string]string{"arm64": "linux/arm64", "amd64": "linux/amd64", "riscv64": "linux/amd64"} {
		if got, native := buildNativePlatform(architecture); got != platform || native != (architecture != "riscv64") {
			t.Error(architecture, got, native)
		}
	}
}

func TestBuildMultiPlatformJoinsOneNodePerArchitecture(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("--platform", "linux/amd64,linux/arm64", "--platform", "linux/amd64", "--push", "-t", "img", "."); code != 0 {
		t.Fatal(code, f.stderr)
	}
	create, build := f.buildx("create"), f.buildx("build")
	if len(f.bodies) != 2 || len(create) != 2 || slices.Contains(create[0], "--append") || !slices.Contains(create[1], "--append") || !slices.Contains(create[1], "linux/arm64") {
		t.Fatal(f.bodies, create)
	}
	if build[0][6] != "linux/amd64,linux/arm64" || slices.Contains(build[0], "--load") || !strings.Contains(build[0][8], "scope=avr-dockerfile-linux-amd64-linux-arm64") {
		t.Fatal(build[0])
	}
	if !f.deleted("bs-1") || !f.deleted("bs-2") {
		t.Fatal(f.requests)
	}
}

func TestBuildRejectsBadInputBeforeAnyRequest(t *testing.T) {
	for _, tc := range []struct {
		args []string
		text string
	}{
		{[]string{"--platform", "linux/s390x", "."}, "unsupported platform 'linux/s390x'"},
		{[]string{"--platform", "linux/amd64,linux/arm64", "."}, "Multi-platform builds produce a manifest list"},
		{[]string{"--builder", "mine", "."}, "--builder is managed by avr build"},
		{[]string{"--builder=mine", "."}, "--builder is managed by avr build"},
		{[]string{"--repo", "", "."}, "--repo cannot be empty"},
		{[]string{"--size", "3-vcpu", "."}, "'--size'"},
		{[]string{"--ttl", "10s", "."}, "--ttl"},
		{[]string{"--ttl", "9h", "."}, "--ttl"},
		{[]string{"--engine", "nerdctl", "."}, "'--engine'"},
		{[]string{"--provision-timeout", "0", "."}, "--provision-timeout"},
	} {
		f := newBuildFixture(t)
		if code := f.run(tc.args...); code != 2 || !strings.Contains(f.stderr.String(), tc.text) || len(f.requests) != 0 || len(f.tools) != 0 {
			t.Errorf("%v: code=%d requests=%v tools=%v\n%s", tc.args, code, f.requests, f.tools, f.stderr)
		}
	}
}

func TestBuildExitCodeAndTeardown(t *testing.T) {
	f := newBuildFixture(t)
	f.buildCode = 17
	if code := f.run("."); code != 17 || len(f.buildx("rm")) != 1 || !f.deleted("bs-1") {
		t.Fatal(code, f.tools, f.requests, f.stderr)
	}

	f = newBuildFixture(t)
	if code := f.run("--keep-sessions", "."); code != 0 || len(f.buildx("rm")) != 1 || f.deleted("bs-1") || !strings.Contains(f.stderr.String(), "Keeping builder session(s) bs-1 running (--keep-sessions)") {
		t.Fatal(code, f.requests, f.stderr)
	}
	if _, rest, found := strings.Cut(f.stderr.String(), "Then remove "); found {
		t.Cleanup(func() { _ = os.RemoveAll(strings.TrimSuffix(strings.SplitN(rest, "\n", 2)[0], ".")) })
	}
}

func TestBuildWaitsThroughTransientPollFailures(t *testing.T) {
	f := newBuildFixture(t)
	f.poll = func(id string, attempt int) (int, http.Header, string) {
		switch attempt {
		case 1:
			return 429, http.Header{"Retry-After": {"1"}}, `{"detail":"slow down"}`
		case 2:
			return 503, nil, `{}`
		case 3:
			return 200, nil, `{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":"STARTING"}}`
		case 4:
			// RUNNING without endpoints is not ready yet.
			return 200, nil, `{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":"RUNNING"}}`
		}
		return 0, nil, ""
	}
	if code := f.run("."); code != 0 || f.polls["bs-1"] != 5 {
		t.Fatal(code, f.polls, f.stderr)
	}
	if !strings.Contains(f.stderr.String(), "  bs-1 STARTING\n  bs-1 RUNNING\n") {
		t.Fatal(f.stderr)
	}
}

func TestBuildProvisioningFailuresCleanUp(t *testing.T) {
	session := func(state, reason string) string {
		return fmt.Sprintf(`{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":%q,"state_reason":%q}}`, state, reason)
	}
	for _, tc := range []struct {
		name string
		poll func(string, int) (int, http.Header, string)
		text string
	}{
		{"endpoint", func(string, int) (int, http.Header, string) {
			return 200, nil, session("RUNNING", "endpoint_unavailable")
		}, "published no usable SSH endpoint"},
		{"capacity", func(string, int) (int, http.Header, string) { return 200, nil, session("FAILED", "no_capacity") }, "no builder capacity for linux/amd64 right now"},
		{"failed", func(string, int) (int, http.Header, string) { return 200, nil, session("FAILED", "boot_error") }, "builder session bs-1 (linux/amd64) failed to start: boot_error."},
		{"stopped", func(string, int) (int, http.Header, string) { return 200, nil, session("STOPPED", "ttl_expired") }, "stopped unexpectedly: ttl_expired."},
		{"forbidden", func(string, int) (int, http.Header, string) { return 403, nil, `{"detail":"no"}` }, "HTTP 403"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBuildFixture(t)
			f.poll = tc.poll
			if code := f.run("--platform", "linux/amd64", "."); code != 1 || !strings.Contains(f.stderr.String(), tc.text) || !f.deleted("bs-1") || len(f.buildx("create")) != 0 || f.polls["bs-1"] != 1 {
				t.Fatal(code, f.requests, f.stderr)
			}
		})
	}
}

func TestBuildProvisionTimeout(t *testing.T) {
	f := newBuildFixture(t)
	now := time.Unix(1_700_000_000, 0)
	f.d.Now = func() time.Time { return now }
	f.d.Wait = func(_ context.Context, delay time.Duration) error { now = now.Add(delay); return nil }
	f.poll = func(string, int) (int, http.Header, string) {
		return 200, nil, `{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":"PENDING"}}`
	}
	if code := f.run("--provision-timeout", "7", "."); code != 1 || !strings.Contains(f.stderr.String(), "builder session(s) not ready after 7s; raise --provision-timeout") || !f.deleted("bs-1") {
		t.Fatal(code, f.stderr)
	}
	// Polled at 0, 3, 6 and 9 seconds.
	if f.polls["bs-1"] != 4 {
		t.Fatal(f.polls)
	}
}

func TestBuildPartialCreateFailureDeletesEarlierSessions(t *testing.T) {
	f := newBuildFixture(t)
	f.create = func(body map[string]any) (int, string) {
		if body["platform"] == "linux/arm64" {
			return 409, `{"detail":"quota"}`
		}
		return 0, ""
	}
	if code := f.run("--platform", "linux/amd64,linux/arm64", "--push", "."); code != 1 || !f.deleted("bs-1") || !strings.Contains(f.stderr.String(), "Hint: you may be at the builder-session quota") || !strings.Contains(f.stderr.String(), "HTTP 409") {
		t.Fatal(code, f.requests, f.stderr)
	}
}

func TestBuildTunnelFailureCleansUp(t *testing.T) {
	f := newBuildFixture(t)
	f.deadPort = true
	if code := f.run("--platform", "linux/amd64", "."); code != 1 || !strings.Contains(f.stderr.String(), "the tunnel to the linux/amd64 builder did not come up") || !f.deleted("bs-1") || len(f.buildx("create")) != 0 || len(f.buildx("rm")) != 0 {
		t.Fatal(code, f.requests, f.tools, f.stderr)
	}
}

func TestBuildCancelledWhileProvisioningDeletesItsSessions(t *testing.T) {
	f := newBuildFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	f.poll = func(string, int) (int, http.Header, string) {
		cancel()
		return 200, nil, `{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":"PENDING"}}`
	}
	if code := Execute(ctx, []string{"build", "."}, f.d); code != 130 || !f.deleted("bs-1") {
		t.Fatal(code, f.requests, f.stderr)
	}
}

func TestBuildTerminatedWhileProvisioningDeletesItsSessions(t *testing.T) {
	f := newBuildFixture(t)
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	f.poll = func(_ string, attempt int) (int, http.Header, string) {
		// The build is listening for the signal by the time it polls.
		if attempt == 1 {
			if err := self.Signal(syscall.SIGTERM); err != nil {
				t.Error(err)
			}
		}
		return 200, nil, `{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":"PENDING"}}`
	}
	f.d.Wait = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	}
	if code := f.run("."); code != 143 || !f.deleted("bs-1") || strings.Contains(f.stderr.String(), "Error") {
		t.Fatal(code, f.requests, f.stderr)
	}
}

func TestBuildRepositoryFallback(t *testing.T) {
	missing := func(string) (int, string) { return 404, `{"detail":"Repository acme/widgets not found"}` }
	t.Run("auto-detected", func(t *testing.T) {
		f := newBuildFixture(t)
		f.resolve = missing
		if code := f.run("--platform", "linux/amd64,linux/arm64", "--push", "."); code != 0 {
			t.Fatal(code, f.stderr)
		}
		// Resolved once, before any session.
		if len(f.bodies) != 2 || f.bodies[0]["repo"] != nil || f.bodies[1]["repo"] != nil || f.count("GET /orgs/"+inspectionOrg+"/repos/resolve") != 1 {
			t.Fatal(f.bodies, f.requests)
		}
		if !strings.Contains(f.stderr.String(), "Note: repository acme/widgets is not connected to this org") || slices.Contains(f.buildx("build")[0], "--cache-from") {
			t.Fatal(f.stderr, f.buildx("build"))
		}
	})
	for name, prepare := range map[string]func(*buildFixture) []string{
		"flag": func(*buildFixture) []string { return []string{"-R", "acme/widgets", "."} },
		"environment": func(f *buildFixture) []string {
			f.d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg, Repo: "acme/widgets"}, nil
			}
			return []string{"."}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newBuildFixture(t)
			f.resolve = missing
			if code := f.run(prepare(f)...); code != 1 || len(f.bodies) != 0 || !strings.Contains(f.stderr.String(), "acme/widgets") {
				t.Fatal(code, f.bodies, f.stderr)
			}
		})
	}
	t.Run("repository ID", func(t *testing.T) {
		// AVR_REPO may name the repository by ID; sessions get its name.
		f := newBuildFixture(t)
		f.d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg, Repo: buildRepositoryID}, nil
		}
		if code := f.run("."); code != 0 || f.bodies[0]["repo"] != "acme/widgets" || f.count("GET /orgs/"+inspectionOrg+"/repos/resolve") != 0 {
			t.Fatal(code, f.bodies, f.requests, f.stderr)
		}
		if build := strings.Join(f.buildx("build")[0], " "); !strings.Contains(build, "token="+buildCacheToken("main")+",") {
			t.Fatal(build)
		}
	})
	t.Run("session 404", func(t *testing.T) {
		f := newBuildFixture(t)
		f.create = func(map[string]any) (int, string) { return 404, `{"detail":"Organization not found"}` }
		if code := f.run("."); code != 1 || len(f.bodies) != 1 {
			t.Fatal(code, f.bodies, f.stderr)
		}
	})
	t.Run("no repository", func(t *testing.T) {
		f := newBuildFixture(t)
		f.d.GitRemote = func(context.Context) (string, error) { return "", os.ErrNotExist }
		if code := f.run("."); code != 0 || f.bodies[0]["repo"] != nil || slices.Contains(f.buildx("build")[0], "--cache-from") || f.count("GET /orgs/"+inspectionOrg+"/repos/resolve") != 0 {
			t.Fatal(code, f.bodies, f.tools, f.stderr)
		}
	})
}

func TestBuildLocalDefaults(t *testing.T) {
	for _, tc := range []struct {
		args []string
		load bool
	}{
		{[]string{"."}, true},
		{[]string{"--load", "."}, true},
		{[]string{"--push", "."}, false},
		{[]string{"--output", "type=cacheonly", "."}, false},
		{[]string{"-o", "type=local,dest=out", "."}, false},
		{[]string{"--output=type=registry", "."}, false},
	} {
		f := newBuildFixture(t)
		if code := f.run(tc.args...); code != 0 {
			t.Fatal(code, f.stderr)
		}
		build := f.buildx("build")[0]
		loads := 0
		for _, arg := range build {
			if arg == "--load" {
				loads++
			}
		}
		if (loads == 1) != tc.load || build[len(build)-1] != "." {
			t.Errorf("%v: %v", tc.args, build)
		}
	}
	// Without a context the current directory is used, and its Containerfile.
	f := newBuildFixture(t)
	if err := os.WriteFile("Containerfile", []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := f.run("-t", "img"); code != 0 {
		t.Fatal(code, f.stderr)
	}
	build := f.buildx("build")[0]
	if got := strings.Join(build[len(build)-6:], " "); got != "-f Containerfile --load -t img ." || !strings.Contains(build[8], "scope=avr-containerfile-linux-") {
		t.Fatal(build)
	}
}

func TestBuildUserCacheFlagsReplaceTheDefaults(t *testing.T) {
	for _, flag := range []string{"--cache-from=type=registry,ref=r/c", "--cache-to=type=inline"} {
		f := newBuildFixture(t)
		if code := f.run(flag, "."); code != 0 {
			t.Fatal(code, f.stderr)
		}
		if build := f.buildx("build")[0]; strings.Contains(strings.Join(build, " "), "type=gha") {
			t.Error(build)
		}
	}
}

func TestBuildForwardsOutputShorthandAndKeepsOrg(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("--org", inspectionOrg, "-o", "type=local,dest=out", "-q", "."); code != 0 {
		t.Fatal(code, f.stderr)
	}
	build := f.buildx("build")[0]
	if got := strings.Join(build[len(build)-4:], " "); got != "-o type=local,dest=out -q ." {
		t.Fatal(build)
	}
}

func TestBuildHelpNeedsNoToolsOrRequests(t *testing.T) {
	f := newBuildFixture(t)
	f.programs = nil
	if code := f.run("--help"); code != 0 || !strings.Contains(f.out.String(), "Usage: avr build [OPTIONS] [BUILDX_ARGS]...") || len(f.requests)+len(f.tools) != 0 {
		t.Fatal(code, f.out, f.stderr)
	}
}

func TestBuildTeardownWarnsAndContinues(t *testing.T) {
	f := newBuildFixture(t)
	f.tool = func(command ToolCommand) (ToolResult, error, bool) {
		if slices.Contains(command.Args, "rm") {
			return ToolResult{Code: 1, Stderr: "no builder\n"}, nil, true
		}
		return ToolResult{}, nil, false
	}
	transport := f.d.HTTP.Transport
	f.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete {
			f.requests = append(f.requests, "DELETE "+r.URL.Path)
			return buildJSONResponse(r, 409, nil, `{"detail":"maintenance"}`), nil
		}
		return transport.RoundTrip(r)
	})}
	if code := f.run("."); code != 0 {
		t.Fatal(code, f.stderr)
	}
	for _, text := range []string{"Warning: failed to remove buildx builder avr-bs-1: no builder", "Warning: failed to stop builder session bs-1 (its TTL will reclaim it): HTTP 409"} {
		if !strings.Contains(f.stderr.String(), text) {
			t.Errorf("missing %q in %s", text, f.stderr)
		}
	}
}

func TestBuildPodmanUsesItsSocketAndCredentials(t *testing.T) {
	f := newBuildFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	socket := filepath.Join(home, "podman.sock")
	auth := filepath.Join(home, "auth.json")
	for path, text := range map[string]string{
		socket: "",
		auth:   `{"auths":{"registry.example":{"auth":"cG9kbWFu"},"shared.example":{"auth":"cG9kbWFu"}}}`,
		filepath.Join(home, ".docker", "config.json"): `{"auths":{"docker.example":{"auth":"ZG9ja2Vy"},"shared.example":{"auth":"ZG9ja2Vy"}},"credsStore":"desktop"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.d.Getenv = func(name string) string {
		if name == "REGISTRY_AUTH_FILE" {
			return auth
		}
		return ""
	}
	f.programs = map[string]string{"podman": "/fixture/podman", "docker": "/fixture/docker", "docker-buildx": "/fixture/docker-buildx"}
	var merged string
	f.tool = func(command ToolCommand) (ToolResult, error, bool) {
		if command.Args[0] == "podman" {
			return ToolResult{Stdout: "unix://" + socket + "\n"}, nil, true
		}
		if slices.Contains(command.Args, "build") {
			for _, entry := range command.Env {
				if path, ok := strings.CutPrefix(entry, "DOCKER_CONFIG="); ok {
					data, err := os.ReadFile(filepath.Join(path, "config.json"))
					if err != nil {
						t.Error(err)
					}
					merged = string(data)
				}
			}
		}
		return ToolResult{}, nil, false
	}
	if code := f.run("."); code != 0 {
		t.Fatal(code, f.stderr)
	}
	if !strings.Contains(f.stderr.String(), "Using podman (/fixture/docker-buildx)") {
		t.Fatal(f.stderr)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(merged), &config); err != nil {
		t.Fatal(err, merged)
	}
	auths, _ := config["auths"].(map[string]any)
	if len(auths) != 3 || fmt.Sprint(auths["shared.example"]) != "map[auth:cG9kbWFu]" || config["credsStore"] != "desktop" {
		t.Fatal(merged)
	}
	for _, command := range f.tools {
		if command.Args[0] != "/fixture/docker-buildx" || command.Args[1] == "version" {
			continue
		}
		// Create, build and rm share the private configuration and socket.
		if !slices.Contains(command.Env, "DOCKER_HOST=unix://"+socket) || !slices.ContainsFunc(command.Env, func(entry string) bool { return strings.HasPrefix(entry, "DOCKER_CONFIG=") }) {
			t.Error(command.Args, "lacks the podman environment")
		}
	}
	if got := f.tools[len(f.tools)-1].Args; strings.Join(got, " ") != "/fixture/docker-buildx rm avr-bs-1" {
		t.Fatal(got)
	}
}

func TestBuildKeyIsReadableByOpenSSH(t *testing.T) {
	directory := t.TempDir()
	path, public, err := buildGenerateKey(directory)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 && runtime.GOOS != "windows" {
		t.Fatal(info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n")) || !strings.HasPrefix(public, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI") || !strings.HasSuffix(public, " avr-build") {
		t.Fatal(string(data), public, err)
	}
	d := Dependencies{}.defaults()
	keygen, err := d.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is not installed")
	}
	result, err := d.RunTool(t.Context(), ToolCommand{Args: []string{keygen, "-y", "-f", path}})
	if err != nil || result.Code != 0 {
		t.Fatal(result, err)
	}
	// ssh-keygen prints the comment only in some versions.
	if got := strings.Fields(result.Stdout); len(got) < 2 || strings.Join(got[:2], " ") != strings.Join(strings.Fields(public)[:2], " ") {
		t.Fatal(result.Stdout, public)
	}
}

func TestBuildCacheTokenMirrorsTheProxyPolicy(t *testing.T) {
	// The values the Python client produced for the same inputs.
	const withMain = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJhYyI6Ilt7XCJTY29wZVwiOlwicmVmcy9oZWFkcy9hdnJlYS9idWlsZGVyLXNlc3Npb25cIixcIlBlcm1pc3Npb25cIjozfSx7XCJTY29wZVwiOlwicmVmcy9oZWFkcy9tYWluXCIsXCJQZXJtaXNzaW9uXCI6MX1dIiwibmJmIjoxNzA0MDY3MjAwLCJleHAiOjQxMDI0NDQ4MDB9."
	const writeOnly = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJhYyI6Ilt7XCJTY29wZVwiOlwicmVmcy9oZWFkcy9hdnJlYS9idWlsZGVyLXNlc3Npb25cIixcIlBlcm1pc3Npb25cIjozfV0iLCJuYmYiOjE3MDQwNjcyMDAsImV4cCI6NDEwMjQ0NDgwMH0."
	for branch, want := range map[string]string{"main": withMain, "": writeOnly, "avrea/builder-session": writeOnly} {
		if got := buildCacheToken(branch); got != want {
			t.Errorf("%q: %s", branch, got)
		}
	}
}

func TestBuildCacheScope(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	service := filepath.Join(root, "svc", "api")
	if err := os.MkdirAll(service, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Dockerfile", "Containerfile.dev"} {
		if err := os.WriteFile(filepath.Join(service, name), []byte("FROM scratch\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d, _, _ := testDeps(t)
	d = d.defaults()
	inGit := true
	d.RunTool = func(_ context.Context, command ToolCommand) (ToolResult, error) {
		if !inGit || command.Args[0] != "git" || command.Args[2] != service {
			return ToolResult{Code: 128}, nil
		}
		return ToolResult{Stdout: root + "\n"}, nil
	}
	amd := []string{"linux/amd64"}
	for _, tc := range []struct {
		args      []string
		platforms []string
		want      string
	}{
		{[]string{service}, amd, "avr-svc-api-dockerfile-linux-amd64"},
		{[]string{"-f", filepath.Join(service, "Dockerfile"), root}, amd, "avr-svc-api-dockerfile-linux-amd64"},
		{[]string{"--file=" + filepath.Join(service, "Containerfile.dev"), service}, amd, "avr-svc-api-containerfile.dev-linux-amd64"},
		{[]string{"--target", "Release_Build", service}, []string{"linux/amd64", "linux/arm64"}, "avr-svc-api-dockerfile-release_build-linux-amd64-linux-arm64"},
		{[]string{"-f", "-", service}, amd, "avr-stdin-linux-amd64"},
		{[]string{"https://github.com/acme/widgets.git"}, amd, "avr-dockerfile-linux-amd64"},
		{[]string{"-f", "missing/Dockerfile.x", service}, amd, "avr-missing-dockerfile.x-linux-amd64"},
	} {
		if got := buildCacheScope(t.Context(), d, parseBuildArgs(tc.args), tc.platforms); got != tc.want {
			t.Errorf("%v: %s != %s", tc.args, got, tc.want)
		}
	}
	inGit = false
	if got := buildCacheScope(t.Context(), d, parseBuildArgs([]string{service}), amd); got != "avr-dockerfile-linux-amd64" {
		t.Error(got)
	}
}

func TestBuildArgumentHelpers(t *testing.T) {
	directory := t.TempDir()
	for _, tc := range []struct {
		args    []string
		context string
	}{
		{[]string{"-t", "img"}, ""},
		{[]string{"-t", "img", "."}, "."},
		{[]string{".", "-t", "img"}, "."},
		{[]string{"-timg", "ctx"}, "ctx"},
		{[]string{"-qfDockerfile", "ctx"}, "ctx"},
		{[]string{"--tag=img", "--no-cache"}, ""},
		{[]string{"--build-arg", "A=1", "-"}, "-"},
		{[]string{"--push", "ctx"}, "ctx"},
		{[]string{"-m", "2g", "--security-opt", "seccomp=x", "ctx"}, "ctx"},
		{[]string{"--", "-ctx"}, "-ctx"},
	} {
		if got := parseBuildArgs(tc.args); got.context != tc.context || got.hasContext != (tc.context != "") {
			t.Errorf("context %v: %+v", tc.args, got)
		}
	}
	for _, tc := range []struct {
		args         []string
		output, load bool
	}{
		{[]string{"--load"}, false, true},
		{[]string{"--load=false"}, false, false},
		{[]string{"--output", "type=docker"}, true, true},
		{[]string{"-o=type=docker,name=x"}, true, true},
		{[]string{"-otype=docker"}, true, true},
		{[]string{"--output=type=docker,dest=img.tar"}, true, false},
		{[]string{"-otype=local,dest=out"}, true, false},
		{[]string{"--push"}, true, false},
		{[]string{"--push=true"}, true, false},
		{[]string{"--push=false"}, false, false},
		{[]string{"--push", "--push=0"}, false, false},
	} {
		parsed := parseBuildArgs(tc.args)
		if parsed.hasOutput() != tc.output || parsed.wantsLocalLoad() != tc.load {
			t.Errorf("%v: output=%v load=%v", tc.args, parsed.hasOutput(), parsed.wantsLocalLoad())
		}
	}
	for _, tc := range []struct {
		args []string
		load bool
	}{
		{[]string{"."}, true},
		{[]string{"--push=false", "."}, true},
		{[]string{"--load=false", "."}, false},
		{[]string{"-otype=local,dest=out", "."}, false},
	} {
		if got := parseBuildArgs(tc.args).outputArgs() != nil; got != tc.load {
			t.Errorf("--load for %v: %v", tc.args, got)
		}
	}
	// A Dockerfile wins over a Containerfile, and -f over both.
	if err := os.WriteFile(filepath.Join(directory, "containerfile"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-t", "x", directory}, {directory, "-t", "x"}} {
		if got := parseBuildArgs(args).dockerfileArgs(); len(got) != 2 || got[1] != filepath.Join(directory, "containerfile") {
			t.Fatal(args, got)
		}
	}
	for _, args := range [][]string{{"-f", "other", directory}, {"-fother", directory}, {"--file=other", directory}} {
		if got := parseBuildArgs(args).dockerfileArgs(); got != nil {
			t.Fatal(args, got)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "Dockerfile"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := parseBuildArgs([]string{directory}).dockerfileArgs(); got != nil {
		t.Fatal(got)
	}
	if value, found := parseBuildArgs([]string{"--target", "a", "--target=b"}).value("--target"); !found || value != "b" {
		t.Fatal(value, found)
	}
	if value := strconv.Quote(parseBuildArgs([]string{"git@github.com:acme/x.git"}).contextDirectory()); value != `""` {
		t.Fatal(value)
	}
}

func TestBuildForwardsAttachedShortValues(t *testing.T) {
	// h and R inside a forwarded cluster are not avr's -h and -R.
	f := newBuildFixture(t)
	if code := f.run("-tghcr.io/acme/app:1", "--push", "-fDockerfile.Release", "."); code != 0 || strings.Contains(f.out.String(), "Usage:") {
		t.Fatal(code, f.out, f.stderr)
	}
	build := f.buildx("build")[0]
	if got := strings.Join(build[len(build)-4:], " "); got != "-tghcr.io/acme/app:1 --push -fDockerfile.Release ." || slices.Contains(build, "--load") {
		t.Fatal(build)
	}
}

func TestBuildRefusesAnUnpinnedBuilder(t *testing.T) {
	f := newBuildFixture(t)
	f.poll = func(id string, _ int) (int, http.Header, string) {
		return 200, nil, fmt.Sprintf(`{"data":{"builder_session_id":%q,"platform":"linux/amd64","state":"RUNNING","endpoints":{"ssh":{"external_ip":"203.0.113.5","external_port":2222,"username":"runner"}}}}`, id)
	}
	if code := f.run("--platform", "linux/amd64", "."); code != 1 || !strings.Contains(f.stderr.String(), "published no SSH host key; refusing") || !f.deleted("bs-1") {
		t.Fatal(code, f.stderr)
	}
	if _, err := os.Stat(f.sshArgs); !os.IsNotExist(err) {
		t.Fatal("ssh ran without a pinned host key")
	}
}

func TestBuildRetriesATunnelWhileSSHDStarts(t *testing.T) {
	f := newBuildFixture(t)
	dead := filepath.Join(f.directory, "dead")
	if err := os.WriteFile(dead, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waits := 0
	f.d.Wait = func(ctx context.Context, delay time.Duration) error {
		// The first ssh was refused; the next one gets through.
		if delay == buildTunnelRetryDelay {
			waits++
			_ = os.Remove(dead)
		}
		return ctx.Err()
	}
	if code := f.run("--platform", "linux/amd64", "."); code != 0 || waits != 1 || len(f.buildx("build")) != 1 {
		t.Fatal(code, waits, f.stderr)
	}
}

func TestBuildRetriesATransientSessionDelete(t *testing.T) {
	f := newBuildFixture(t)
	transport := f.d.HTTP.Transport
	deletes := 0
	f.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete {
			if deletes++; deletes == 1 {
				return buildJSONResponse(r, 503, nil, `{}`), nil
			}
		}
		return transport.RoundTrip(r)
	})}
	if code := f.run("."); code != 0 || deletes != 2 || !f.deleted("bs-1") || strings.Contains(f.stderr.String(), "Warning:") {
		t.Fatal(code, deletes, f.stderr)
	}
}

func TestBuildKeepSessionsKeepsTheKey(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("--keep-sessions", "."); code != 0 || f.deleted("bs-1") {
		t.Fatal(code, f.stderr)
	}
	text := f.stderr.String()
	_, login, found := strings.Cut(text, "Reach them with:\n  bs-1: ")
	if !found {
		t.Fatal(text)
	}
	argv := strings.Fields(strings.SplitN(login, "\n", 2)[0])
	key := argv[slices.Index(argv, "-i")+1]
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(key)) })
	if _, err := os.Stat(key); err != nil || !slices.Contains(argv, "StrictHostKeyChecking=yes") || argv[len(argv)-1] != "runner@203.0.113.5" {
		t.Fatal(argv, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(key), "buildx")); !os.IsNotExist(err) {
		t.Fatal("builder state kept", err)
	}
}

func TestBuildForwardsTheDoubleDash(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("-t", "img", "--", "-ctx"); code != 0 {
		t.Fatal(code, f.stderr)
	}
	build := f.buildx("build")[0]
	// Nothing after `--` is a flag, and the context given there stays the
	// only one.
	if got := strings.Join(build[len(build)-4:], " "); got != "-t img -- -ctx" || slices.Index(build, "--load") > slices.Index(build, "--") {
		t.Fatal(build)
	}
}

func TestBuildEvaluationNeedsNoOutput(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("--platform", "linux/amd64,linux/arm64", "--check"); code != 0 {
		t.Fatal(code, f.stderr)
	}
	if build := f.buildx("build")[0]; slices.Contains(build, "--load") || build[len(build)-1] != "." {
		t.Fatal(build)
	}
	f = newBuildFixture(t)
	if code := f.run("--call=targets"); code != 0 || slices.Contains(f.buildx("build")[0], "--load") {
		t.Fatal(code, f.buildx("build"), f.stderr)
	}
	// --resource takes a value, which is not the context.
	f = newBuildFixture(t)
	if code := f.run("--resource", "memory=2g"); code != 0 {
		t.Fatal(code, f.stderr)
	}
	if build := f.buildx("build")[0]; strings.Join(build[len(build)-3:], " ") != "--resource memory=2g ." {
		t.Fatal(build)
	}
}

func TestBuildTunnelIgnoresUserSSHTrust(t *testing.T) {
	f := newBuildFixture(t)
	if code := f.run("."); code != 0 {
		t.Fatal(code, f.stderr)
	}
	data, err := os.ReadFile(f.sshArgs)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSpace(string(data)), "\n")
	if argv[0] != "-F" || argv[1] != "none" {
		t.Fatal(argv)
	}
	for _, option := range []string{"GlobalKnownHostsFile=none", "ControlMaster=no", "ControlPath=none", "StrictHostKeyChecking=yes", "BatchMode=yes"} {
		if !slices.Contains(argv, option) {
			t.Error(option, argv)
		}
	}
}

func TestBuildSecondTunnelFailureTearsDownTheFirst(t *testing.T) {
	f := newBuildFixture(t)
	if err := os.WriteFile(filepath.Join(f.directory, "dead-1"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := f.run("--platform", "linux/amd64,linux/arm64", "--push", "."); code != 1 || !strings.Contains(f.stderr.String(), "the tunnel to the linux/arm64 builder did not come up") || !f.deleted("bs-1") || !f.deleted("bs-2") || len(f.buildx("create")) != 0 {
		t.Fatal(code, f.requests, f.stderr)
	}
	if !strings.Contains(f.stderr.String(), "Builder ready: bs-1 (linux/amd64)") {
		t.Fatal(f.stderr)
	}
}

func TestBuildKeepSessionsAfterAFailure(t *testing.T) {
	t.Run("never reachable", func(t *testing.T) {
		f := newBuildFixture(t)
		f.poll = func(string, int) (int, http.Header, string) {
			return 200, nil, `{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":"FAILED","state_reason":"boot_error"}}`
		}
		if code := f.run("--keep-sessions", "."); code != 1 || !f.deleted("bs-1") || !strings.Contains(f.stderr.String(), "Stopping builder session(s) bs-1 despite --keep-sessions") || strings.Contains(f.stderr.String(), "Keeping") {
			t.Fatal(code, f.requests, f.stderr)
		}
	})
	t.Run("tunnel down", func(t *testing.T) {
		f := newBuildFixture(t)
		f.deadPort = true
		if code := f.run("--keep-sessions", "."); code != 1 || f.deleted("bs-1") {
			t.Fatal(code, f.requests, f.stderr)
		}
		// Why the tunnel did not come up is what the login is for.
		_, login, found := strings.Cut(f.stderr.String(), "Reach them with:\n  bs-1: ")
		if !found {
			t.Fatal(f.stderr)
		}
		argv := strings.Fields(strings.SplitN(login, "\n", 2)[0])
		key := argv[slices.Index(argv, "-i")+1]
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(key)) })
		if _, err := os.Stat(key); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("one of two reachable", func(t *testing.T) {
		f := newBuildFixture(t)
		f.poll = func(id string, _ int) (int, http.Header, string) {
			if id == "bs-2" {
				return 200, nil, `{"data":{"builder_session_id":"bs-2","platform":"linux/arm64","state":"FAILED"}}`
			}
			return 0, nil, ""
		}
		if code := f.run("--keep-sessions", "--platform", "linux/amd64,linux/arm64", "--push", "."); code != 1 || f.deleted("bs-1") || !f.deleted("bs-2") {
			t.Fatal(code, f.requests, f.stderr)
		}
		text := f.stderr.String()
		if !strings.Contains(text, "Keeping builder session(s) bs-1 running") || !strings.Contains(text, "Reach them with:\n  bs-1: ") {
			t.Fatal(text)
		}
		if _, rest, found := strings.Cut(text, "Then remove "); found {
			t.Cleanup(func() { _ = os.RemoveAll(strings.TrimSuffix(strings.SplitN(rest, "\n", 2)[0], ".")) })
		}
	})
}

func TestBuildHangupDeletesItsSessions(t *testing.T) {
	f := newBuildFixture(t)
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	f.poll = func(_ string, attempt int) (int, http.Header, string) {
		if attempt == 1 {
			if err := self.Signal(syscall.SIGHUP); err != nil {
				t.Error(err)
			}
		}
		return 200, nil, `{"data":{"builder_session_id":"bs-1","platform":"linux/amd64","state":"PENDING"}}`
	}
	f.d.Wait = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	}
	if code := f.run("."); code != 129 || !f.deleted("bs-1") {
		t.Fatal(code, f.requests, f.stderr)
	}
}

func TestBuildRefusesATemporaryDirectoryInsideTheContext(t *testing.T) {
	f := newBuildFixture(t)
	inside, err := filepath.Abs("tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", inside)
	if code := f.run("."); code != 1 || len(f.requests) != 0 || !strings.Contains(f.stderr.String(), "set TMPDIR outside the build context") {
		t.Fatal(code, f.requests, f.stderr)
	}
}

func TestBuildScopesTheCacheToTheContextRepository(t *testing.T) {
	f := newBuildFixture(t)
	sibling := t.TempDir()
	f.tool = func(command ToolCommand) (ToolResult, error, bool) {
		if command.Args[0] == "git" && slices.Contains(command.Args, "get-url") {
			if command.Args[2] != sibling {
				t.Error(command.Args)
			}
			return ToolResult{Stdout: "git@github.com:acme/gadgets.git\n"}, nil, true
		}
		return ToolResult{}, nil, false
	}
	var resolved []string
	f.resolve = func(name string) (int, string) {
		resolved = append(resolved, name)
		return 0, ""
	}
	if code := f.run(sibling); code != 0 || !slices.Equal(resolved, []string{"acme/gadgets"}) {
		t.Fatal(code, resolved, f.stderr)
	}
	// A context outside any repository builds without one.
	f = newBuildFixture(t)
	if code := f.run(t.TempDir()); code != 0 || f.count("GET /orgs/"+inspectionOrg+"/repos/resolve") != 0 || f.bodies[0]["repo"] != nil {
		t.Fatal(code, f.requests, f.stderr)
	}
}

func TestBuildDeleteChecksAnAmbiguousNotFound(t *testing.T) {
	for _, tc := range []struct {
		name  string
		gone  bool
		warns bool
	}{{"gone", true, false}, {"still active", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBuildFixture(t)
			transport := f.d.HTTP.Transport
			deleted := false
			f.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.Method == http.MethodDelete:
					deleted = true
					return buildJSONResponse(r, 404, nil, `{"detail":"not found"}`), nil
				case r.Method == http.MethodGet && deleted && tc.gone:
					return buildJSONResponse(r, 404, nil, `{"detail":"not found"}`), nil
				}
				return transport.RoundTrip(r)
			})}
			code := f.run(".")
			if warned := strings.Contains(f.stderr.String(), "Warning: failed to stop builder session bs-1"); code != 0 || warned != tc.warns {
				t.Fatal(code, f.stderr)
			}
		})
	}
}

func TestBuildDeleteHonorsRetryAfter(t *testing.T) {
	f := newBuildFixture(t)
	transport := f.d.HTTP.Transport
	deletes := 0
	f.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete {
			if deletes++; deletes == 1 {
				return buildJSONResponse(r, 429, http.Header{"Retry-After": {"5"}}, `{}`), nil
			}
		}
		return transport.RoundTrip(r)
	})}
	var waited []time.Duration
	f.d.Wait = func(ctx context.Context, delay time.Duration) error {
		waited = append(waited, delay)
		return ctx.Err()
	}
	if code := f.run("."); code != 0 || deletes != 2 || !slices.Contains(waited, 5*time.Second) {
		t.Fatal(code, deletes, waited, f.stderr)
	}
}
