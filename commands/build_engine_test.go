package commands

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestBuildEngineSelection(t *testing.T) {
	const buildx = "/fixture/docker-buildx"
	type tools struct {
		podman, docker, standalone, socket bool
		version                            string
	}
	real := "github.com/docker/buildx v0.20.0"
	for _, tc := range []struct {
		name, choice string
		tools        tools
		args         []string
		engine       string
		text         string
	}{
		{"auto prefers podman", "auto", tools{podman: true, docker: true, standalone: true, socket: true, version: real}, []string{"--load", "."}, "podman", "Using podman (" + buildx + ")"},
		{"auto with docker only", "auto", tools{docker: true, version: real}, []string{"--load", "."}, "docker", "Using docker (docker)"},
		{"docker override", "docker", tools{podman: true, docker: true, standalone: true, socket: true, version: real}, []string{"--load", "."}, "docker", "Using docker (docker)"},
		{"podman without buildx falls back", "auto", tools{podman: true, docker: true, version: real}, []string{"--load", "."}, "docker", "Note: podman found but no standalone buildx; using docker"},
		{"podman alone lacks buildx", "auto", tools{podman: true}, []string{"--load", "."}, "", "podman found but avr build also needs buildx"},
		{"forced podman lacks buildx", "podman", tools{podman: true, docker: true, version: real}, []string{"--load", "."}, "", "podman builds need buildx"},
		{"forced podman is missing", "podman", tools{docker: true, standalone: true, version: real}, []string{"--load", "."}, "", "podman CLI not found"},
		{"nothing installed", "auto", tools{}, []string{"--load", "."}, "", "neither docker nor podman found"},
		{"podman load needs the socket", "auto", tools{podman: true, standalone: true, version: real}, []string{"--load", "."}, "", "needs podman's API socket"},
		{"podman docker exporter needs the socket", "auto", tools{podman: true, standalone: true, version: real}, []string{"--output", "type=docker", "."}, "", "needs podman's API socket"},
		{"auto falls back to docker without podman's socket", "auto", tools{podman: true, docker: true, standalone: true, version: real}, []string{"--load", "."}, "docker", "Note: podman's API socket is not available; using docker to load the image."},
		{"forced podman does not fall back", "podman", tools{podman: true, docker: true, standalone: true, version: real}, []string{"--load", "."}, "", "needs podman's API socket"},
		{"podman push only warns", "auto", tools{podman: true, standalone: true, version: real}, []string{"--push", "."}, "podman", "Warning: podman API socket not found; --load will not work"},
		{"podman-docker shim", "docker", tools{docker: true, version: "buildah 1.37.0"}, []string{"--load", "."}, "", "docker buildx is not available"},
		{"standalone is not buildx", "podman", tools{podman: true, standalone: true, socket: true, version: "something else"}, []string{"--load", "."}, "", "did not run as buildx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d = d.defaults()
			socket := t.TempDir()
			d.LookPath = func(name string) (string, error) {
				switch {
				case name == "podman" && tc.tools.podman, name == "docker" && tc.tools.docker:
					return "/fixture/" + name, nil
				case name == "docker-buildx" && tc.tools.standalone:
					return buildx, nil
				}
				return "", os.ErrNotExist
			}
			d.RunTool = func(_ context.Context, command ToolCommand) (ToolResult, error) {
				switch {
				case command.Args[0] == "podman" && tc.tools.socket:
					return ToolResult{Stdout: socket + "\n"}, nil
				case command.Args[0] == "podman":
					return ToolResult{Code: 125}, nil
				case command.Args[0] == "docker" && !tc.tools.docker:
					return ToolResult{}, errors.New("not found")
				}
				return ToolResult{Stdout: tc.tools.version}, nil
			}
			engine, err := buildResolveEngine(t.Context(), d, tc.choice, parseBuildArgs(tc.args))
			if tc.engine == "" {
				if err == nil || !strings.Contains(err.Error(), tc.text) {
					t.Fatal(engine, err, stderr)
				}
				return
			}
			if err != nil || engine.name != tc.engine || !strings.Contains(stderr.String(), tc.text) {
				t.Fatal(engine, err, stderr)
			}
			if wantHost := tc.engine == "podman" && tc.tools.socket; (engine.dockerHost == "unix://"+socket) != wantHost {
				t.Fatal(engine.dockerHost)
			}
		})
	}
}

func TestBuildEngineKeepsAnExplicitDockerHost(t *testing.T) {
	d, _, _ := testDeps(t)
	d = d.defaults()
	d.Getenv = func(name string) string {
		if name == "DOCKER_HOST" {
			return "unix:///run/custom.sock"
		}
		return ""
	}
	d.LookPath = func(name string) (string, error) { return "/fixture/" + name, nil }
	d.RunTool = func(_ context.Context, command ToolCommand) (ToolResult, error) {
		if command.Args[0] == "podman" {
			t.Error("podman's socket must not be looked up")
		}
		return ToolResult{Stdout: "buildx"}, nil
	}
	engine, err := buildResolveEngine(t.Context(), d, "podman", parseBuildArgs([]string{"--load", "."}))
	if err != nil || engine.dockerHost != "" {
		t.Fatal(engine, err)
	}
}

func TestBuildEngineFromEnvironment(t *testing.T) {
	f := newBuildFixture(t)
	f.programs = map[string]string{"podman": "/fixture/podman", "docker": "/fixture/docker", "docker-buildx": "/fixture/docker-buildx"}
	f.d.Getenv = func(name string) string {
		if name == "AVR_BUILD_ENGINE" {
			return "docker"
		}
		return ""
	}
	if code := f.run("."); code != 0 || !strings.Contains(f.stderr.String(), "Using docker (docker)") {
		t.Fatal(code, f.stderr)
	}
	f = newBuildFixture(t)
	f.d.Getenv = func(name string) string {
		if name == "AVR_BUILD_ENGINE" {
			return "nerdctl"
		}
		return ""
	}
	if code := f.run("."); code != 2 || len(f.requests) != 0 {
		t.Fatal(code, f.stderr)
	}
}

func TestBuildPodmanCredentialsShadowDockerHelpers(t *testing.T) {
	merged := map[string]any{
		"credsStore":  "desktop",
		"credHelpers": map[string]any{"ghcr.io": "gh", "gcr.io": "gcloud"},
		"auths":       map[string]any{"docker.io": map[string]any{}},
	}
	buildMergePodmanAuth(merged, map[string]any{
		"auths":       map[string]any{"ghcr.io": map[string]any{"auth": "cG9k"}, "quay.io/acme": map[string]any{"auth": "cXVheQ=="}},
		"credHelpers": map[string]any{"ecr.example": "ecr-login"},
	})
	helpers := merged["credHelpers"].(map[string]any)
	// Podman's logins reach auths; docker's other helpers and store stay.
	for name, want := range map[string]any{"ghcr.io": "", "quay.io": "", "gcr.io": "gcloud", "ecr.example": "ecr-login"} {
		if helpers[name] != want {
			t.Errorf("%s: %v", name, helpers[name])
		}
	}
	auths := merged["auths"].(map[string]any)
	if merged["credsStore"] != "desktop" || auths["ghcr.io"] == nil || auths["quay.io/acme"] == nil || auths["docker.io"] == nil {
		t.Fatal(merged)
	}
}
