package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
)

// Helpers over the arguments `avr build` forwards to `buildx build`.

var buildPlatforms = []string{"linux/amd64", "linux/arm64"}

// buildNativePlatform is the builder platform matching the client's CPU, as
// `docker build` targets the machine it runs on. Builders are Linux only, so
// the operating system never comes from the client.
func buildNativePlatform(architecture string) (string, bool) {
	platform := "linux/" + architecture
	if slices.Contains(buildPlatforms, platform) {
		return platform, true
	}
	return buildPlatforms[0], false
}

// buildSplitPlatforms merges repeated and comma-separated --platform values,
// in order and without duplicates. No value selects the native platform.
func buildSplitPlatforms(d Dependencies, values []string) ([]string, error) {
	platforms := []string{}
	for _, raw := range values {
		for part := range strings.SplitSeq(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !slices.Contains(buildPlatforms, part) {
				return nil, &ExitError{2, "Error: Invalid value for '--platform': unsupported platform " + pythonQuote(output.CellText(part)) + " (choose from " + strings.Join(buildPlatforms, ", ") + ")"}
			}
			if !slices.Contains(platforms, part) {
				platforms = append(platforms, part)
			}
		}
	}
	if len(platforms) > 0 {
		return platforms, nil
	}
	platform, native := buildNativePlatform(runtime.GOARCH)
	if !native {
		if _, err := d.Err.Write([]byte("Note: no builder matches this machine's architecture (" + runtime.GOARCH + "); building for " + platform + ". Pass --platform to choose.\n")); err != nil {
			return nil, err
		}
	}
	return []string{platform}, nil
}

// `docker buildx build` flags that take a value, by canonical name, including
// the legacy `docker build` ones buildx still accepts. Telling a flag's value
// (`-t img`) from the build context needs them.
var buildValueFlags = []string{
	"--add-host", "--allow", "--annotation", "--attest", "--build-arg", "--build-context", "--builder",
	"--cache-from", "--cache-to", "--call", "--cgroup-parent", "--cpu-period", "--cpu-quota", "--cpu-shares",
	"--cpuset-cpus", "--cpuset-mems", "--file", "--iidfile", "--isolation", "--label", "--memory", "--memory-swap",
	"--metadata-file", "--network", "--no-cache-filter", "--output", "--platform", "--progress", "--provenance",
	"--sbom", "--secret", "--security-opt", "--shm-size", "--ssh", "--tag", "--target", "--ulimit",
}

// Shorthands, to their canonical names. Those not in buildValueFlags are
// booleans.
var buildShorthands = map[byte]string{'c': "--cpu-shares", 'D': "--debug", 'f': "--file", 'm': "--memory", 'o': "--output", 'q': "--quiet", 't': "--tag"}

type buildFlag struct {
	name, value string
	// valued is a value given to a boolean flag (`--push=false`).
	valued bool
}

// buildArgs is the forwarded `buildx build` argv, parsed once for every
// question avr build asks about it.
type buildArgs struct {
	flags []buildFlag
	// context is the positional build context, or "" when none was given.
	context    string
	hasContext bool
}

// parseBuildArgs splits flags the way the docker CLI does: `--flag value`,
// `--flag=value`, and short clusters with an attached value (`-tname`,
// `-qf=Dockerfile`). Unknown long flags are taken as booleans.
func parseBuildArgs(args []string) buildArgs {
	parsed := buildArgs{}
	positional := func(arg string) {
		if !parsed.hasContext {
			parsed.context, parsed.hasContext = arg, true
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			for _, rest := range args[i+1:] {
				positional(rest)
			}
			return parsed
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			positional(arg)
		case strings.HasPrefix(arg, "--"):
			name, value, assigned := strings.Cut(arg, "=")
			if !assigned && slices.Contains(buildValueFlags, name) && i+1 < len(args) {
				i++
				value, assigned = args[i], true
			}
			parsed.flags = append(parsed.flags, buildFlag{name, value, assigned})
		default:
			for j := 1; j < len(arg); j++ {
				name, known := buildShorthands[arg[j]]
				if !known {
					name = "-" + arg[j:j+1]
				}
				if !slices.Contains(buildValueFlags, name) {
					parsed.flags = append(parsed.flags, buildFlag{name: name})
					continue
				}
				value := strings.TrimPrefix(arg[j+1:], "=")
				if j+1 == len(arg) && i+1 < len(args) {
					i++
					value = args[i]
				}
				parsed.flags = append(parsed.flags, buildFlag{name, value, true})
				break
			}
		}
	}
	return parsed
}

// has reports whether any of names was given.
func (a buildArgs) has(names ...string) bool {
	for _, flag := range a.flags {
		if slices.Contains(names, flag.name) {
			return true
		}
	}
	return false
}

// value returns the last value given for name.
func (a buildArgs) value(name string) (string, bool) {
	value, found := "", false
	for _, flag := range a.flags {
		if flag.name == name {
			value, found = flag.value, true
		}
	}
	return value, found
}

// values returns every value given for name, in order.
func (a buildArgs) values(name string) []string {
	values := []string{}
	for _, flag := range a.flags {
		if flag.name == name {
			values = append(values, flag.value)
		}
	}
	return values
}

// enabled reports whether boolean flag name is on: given last as `--name` or
// `--name=<true>`.
func (a buildArgs) enabled(name string) bool {
	on := false
	for _, flag := range a.flags {
		if flag.name != name {
			continue
		}
		on = true
		if flag.valued {
			on, _ = strconv.ParseBool(flag.value)
		}
	}
	return on
}

// hasOutput reports a destination that can hold a manifest list: --push or
// an explicit exporter.
func (a buildArgs) hasOutput() bool {
	return a.enabled("--push") || a.has("--output")
}

// wantsLocalLoad reports arguments that load the result into the local image
// store, which needs a daemon speaking the docker API.
func (a buildArgs) wantsLocalLoad() bool {
	if a.enabled("--load") {
		return true
	}
	for _, value := range a.values("--output") {
		if strings.Contains(value, "type=docker") && !strings.Contains(value, "dest=") {
			return true
		}
	}
	return false
}

// contextDirectory returns the local build context directory. Stdin, remote
// contexts and anything that is not a directory return "".
func (a buildArgs) contextDirectory() string {
	context := a.context
	if !a.hasContext || context == "-" || strings.Contains(context, "://") || strings.HasPrefix(context, "git@") {
		return ""
	}
	if info, err := os.Stat(context); err != nil || !info.IsDir() {
		return ""
	}
	return context
}

// dockerfileArgs selects a Containerfile when the context has one, has no
// Dockerfile, and no -f was given. podman build does that by itself, but
// BuildKit's dockerfile frontend only looks for Dockerfile.
func (a buildArgs) dockerfileArgs() []string {
	if a.has("--file") {
		return nil
	}
	directory := a.contextDirectory()
	if directory == "" {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	if names["Dockerfile"] || names["dockerfile"] {
		return nil
	}
	for _, name := range []string{"Containerfile", "containerfile"} {
		if names[name] {
			return []string{"-f", filepath.Join(directory, name)}
		}
	}
	return nil
}

// outputArgs adds --load when no output was asked for, as `docker build` and
// `podman build` do. With the remote driver the result would otherwise stay
// in the builder VM's cache, which is discarded after the build. An explicit
// --load=false keeps it there.
func (a buildArgs) outputArgs() []string {
	if a.hasOutput() || a.has("--load") {
		return nil
	}
	return []string{"--load"}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func buildGitToplevel(ctx context.Context, d Dependencies, directory string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := d.RunTool(ctx, ToolCommand{Args: []string{"git", "-C", directory, "rev-parse", "--show-toplevel"}})
	if err != nil || result.Code != 0 {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// buildDockerfileName is a stable name for the Dockerfile being built: its
// path from the git top level when it is local, so `-f svc/Dockerfile svc` and
// `cd svc; avr build .` share a cache. Remote contexts and stdin keep the name
// as given.
func buildDockerfileName(ctx context.Context, d Dependencies, args buildArgs) string {
	given, found := args.value("--file")
	if given == "-" {
		return "stdin"
	}
	path := given
	if !found {
		directory := args.contextDirectory()
		if directory == "" {
			return "Dockerfile"
		}
		path = filepath.Join(directory, "Dockerfile")
		if lower := filepath.Join(directory, "dockerfile"); !isRegularFile(path) && isRegularFile(lower) {
			path = lower
		}
	}
	if !isRegularFile(path) {
		if given != "" {
			return given
		}
		return filepath.Base(path)
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if top := buildGitToplevel(ctx, d, filepath.Dir(path)); top != "" {
		if resolved, err := filepath.EvalSymlinks(top); err == nil {
			top = resolved
		}
		if relative, err := filepath.Rel(top, path); err == nil && filepath.IsLocal(relative) {
			return filepath.ToSlash(relative)
		}
	}
	return filepath.Base(path)
}

var buildScopeSeparator = lazyRegexp("[^a-z0-9._]+")

// buildCacheScope names the cache index for this build from the Dockerfile
// path, --target and platforms, so that images and architectures in one
// repository do not overwrite each other's.
func buildCacheScope(ctx context.Context, d Dependencies, args buildArgs, platforms []string) string {
	parts := []string{buildDockerfileName(ctx, d, args)}
	if target, _ := args.value("--target"); target != "" {
		parts = append(parts, target)
	}
	parts = append(parts, platforms...)
	slug := buildScopeSeparator().ReplaceAllString(strings.ToLower(strings.Join(parts, "-")), "-")
	return "avr-" + strings.Trim(slug, "-")
}

// Layer cache: BuildKit's type=gha backend against the Avrea cache proxy,
// which a builder VM reaches like any hosted VM. The proxy scopes entries by
// the VM's source address: writes go to the reserved builder branch and reads
// fall back to the default branch.
const (
	buildCacheURL      = "https://cache.avrea.com/"
	buildCacheWriteRef = "refs/heads/avrea/builder-session"
	// A fixed validity window (2024-01-01 to 2100-01-01) keeps the token the
	// same across builds; BuildKit only checks that the claims exist.
	buildCacheTokenNotBefore = 1704067200
	buildCacheTokenExpires   = 4102444800
)

func buildCompactJSON(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value) // The values are strings and integers.
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
}

// buildCacheToken is an unsigned placeholder: BuildKit's type=gha client
// refuses a token that is not a JWT carrying `ac`, `nbf` and `exp`. It names
// no repository and is not a credential, as the proxy authorizes by source
// address.
//
// BuildKit hashes each `ac` ref into its cache index key, writing under the
// read-write entry and reading under every entry. So `ac` must mirror the
// proxy's policy for the session (the reserved write ref, then the default
// branch read-only), or the read fallback never hits.
func buildCacheToken(defaultBranch string) string {
	type access struct {
		Scope      string
		Permission int
	}
	scopes := []access{{buildCacheWriteRef, 3}}
	if ref := "refs/heads/" + defaultBranch; defaultBranch != "" && ref != buildCacheWriteRef {
		scopes = append(scopes, access{ref, 1})
	}
	claims := struct {
		AC  string `json:"ac"`
		NBF int64  `json:"nbf"`
		EXP int64  `json:"exp"`
	}{string(buildCompactJSON(scopes)), buildCacheTokenNotBefore, buildCacheTokenExpires}
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + encode(buildCompactJSON(claims)) + "."
}

// buildCacheArgs imports and exports the layer cache through the cache proxy,
// unless the user set up remote caching with --cache-from or --cache-to. An
// export failure only warns: a cache outage, or a repository with the cache
// turned off, must not fail a good build.
func buildCacheArgs(ctx context.Context, d Dependencies, args buildArgs, platforms []string, defaultBranch string) []string {
	if args.has("--cache-from", "--cache-to") {
		return nil
	}
	attributes := "type=gha,url_v2=" + buildCacheURL + ",version=2,token=" + buildCacheToken(defaultBranch) + ",scope=" + buildCacheScope(ctx, d, args, platforms)
	return []string{"--cache-from", attributes, "--cache-to", attributes + ",mode=max,ignore-error=true"}
}
