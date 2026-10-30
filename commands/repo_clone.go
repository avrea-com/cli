package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
)

type cloneDiagnosticWriter struct {
	writer io.Writer
	mutex  *sync.Mutex
}

func (w cloneDiagnosticWriter) Write(p []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.writer.Write(p)
}

var cloneRepositoryID = regexp.MustCompile("^rep-[0-9a-f]{32}$")

func mirrorEndpoint(raw string) (string, error) {
	raw = strings.TrimRight(raw, "/")
	// Reuse the credential adapter's authority and secure-transport checks.
	_, err := gitURL(raw+"/owner/repo.git", "owner/repo")
	if err != nil {
		return "", &ExitError{1, "Error: The API returned an invalid or insecure mirror endpoint."}
	}
	return raw, nil
}
func issueGitToken(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, fullName string) (string, time.Time, error) {
	record, err := api.GitToken(ctx, fullName)
	if err != nil {
		var he *client.HTTPError
		var te *client.TransportError
		switch {
		case errors.As(err, &he):
			return "", time.Time{}, apiError(cfg, err, "issue a Git token")
		case errors.As(err, &te):
			return "", time.Time{}, &ExitError{1, "Error: Could not reach Avrea to issue a Git token."}
		default:
			if ctx.Err() != nil {
				return "", time.Time{}, ctx.Err()
			}
			return "", time.Time{}, &ExitError{1, "Error: Avrea returned an invalid or expired Git token."}
		}
	}
	token := fieldText(record.Fields, "token")
	expires, err := time.Parse(time.RFC3339Nano, fieldText(record.Fields, "expires_at"))
	if err != nil || !expires.After(d.Now()) || !repoPart.MatchString(token) {
		return "", time.Time{}, &ExitError{1, "Error: Avrea returned an invalid or expired Git token."}
	}
	return token, expires, nil
}
func cloneURL(endpoint, repo string) string { return endpoint + "/" + repo + ".git" }
func selectMirror(endpoints []string, requested string) (string, error) {
	var matches []string
	for _, ep := range endpoints {
		u, _ := url.Parse(ep)
		if value := strings.TrimRight(requested, "/"); value == ep || value == u.Host {
			matches = append(matches, ep)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", &ExitError{2, "Error: Invalid value for --placement: must match an advertised mirror URL or hostname. Available placements: " + strings.Join(endpoints, ", ")}
}
func nearestMirror(ctx context.Context, transport *http.Client, endpoints []string, fullName, token string) (string, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	h := http.Client{}
	if transport != nil {
		h = *transport
	}
	h.Timeout = 5 * time.Second
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// A probe authenticates with its Git JWT only, independently of API cookies.
	h.Jar = nil
	type result struct{ endpoint, reason string }
	results := make(chan result, len(endpoints))
	for _, ep := range endpoints {
		go func() {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, cloneURL(ep, fullName)+"/info/refs?service=git-upload-pack", nil)
			if err != nil {
				results <- result{ep, "ConnectError"}
				return
			}
			request.Header.Set("Authorization", "Bearer "+token)
			response, err := h.Do(request)
			reason := ""
			if err != nil {
				reason = "ConnectError"
				var network net.Error
				if errors.As(err, &network) && network.Timeout() {
					reason = "ReadTimeout"
				}
			} else {
				_ = response.Body.Close()
				if response.StatusCode != 200 || strings.Split(response.Header.Get("Content-Type"), ";")[0] != "application/x-git-upload-pack-advertisement" {
					reason = fmt.Sprintf("HTTP %d", response.StatusCode)
				}
			}
			results <- result{ep, reason}
		}()
	}
	failures := map[string]string{}
	for range endpoints {
		select {
		case result := <-results:
			if result.reason == "" {
				return result.endpoint, nil
			}
			failures[result.endpoint] = result.reason
		case <-ctx.Done():
			if parent.Err() != nil {
				return "", parent.Err()
			}
			return "", mirrorProbeFailure(endpoints, failures)
		}
	}
	return "", mirrorProbeFailure(endpoints, failures)
}
func mirrorProbeFailure(endpoints []string, failures map[string]string) error {
	var details, hints []string
	permissions, notReady, unavailable := false, false, len(failures) < len(endpoints)
	for _, ep := range endpoints {
		reason := failures[ep]
		if reason == "" {
			reason = "timed out"
		}
		details = append(details, ep+": "+reason)
		permissions = permissions || reason == "HTTP 401" || reason == "HTTP 403"
		notReady = notReady || reason == "HTTP 404"
		unavailable = unavailable || strings.HasPrefix(reason, "HTTP 5") || !strings.HasPrefix(reason, "HTTP ")
	}
	if permissions {
		hints = append(hints, "Check your Avrea login and repository permissions.")
	}
	if notReady {
		hints = append(hints, "Check \x60avr repo mirror status\x60 and retry after the repository has synced.")
	}
	if unavailable {
		hints = append(hints, "Some mirrors are unreachable or unavailable; check connectivity and \x60avr health\x60.")
	}
	if len(hints) == 0 {
		hints = append(hints, "The mirrors did not return a Git advertisement; check \x60avr health\x60.")
	}
	return &ExitError{1, "Error: No mirror could serve this repository (" + strings.Join(details, "; ") + "). " + strings.Join(hints, " ")}
}
func cloneCredentialConfig(executable, apiHost, target, repo string) []string {
	// Persist the running native executable: an avr found on PATH may still be
	// the Python CLI, and Git invokes this helper from a different directory.
	command := "AVR_HOST=" + shellquote.Join(apiHost) + " " + shellquote.Join(executable, "auth", "git-credential", "--url", target, "--repo", repo)
	helper := "!f() { case \"$1\" in store|erase) return 0;; esac; " + command + " \"$@\" || { printf \"quit=1\\n\\n\"; exit 1; }; }; f"
	return []string{"credential." + target + ".helper=", "credential." + target + ".helper=" + helper, "credential." + target + ".useHttpPath=true", "http." + target + ".followRedirects=false"}
}
func cloneEnvironment() []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "GIT_TRACE") || key == "GIT_CURL_VERBOSE" || key == "GIT_TERMINAL_PROMPT" || key == "GIT_ASKPASS" || key == "SSH_ASKPASS" || key == "AVR_GIT_CLONE_AUTHORIZATION" {
			continue
		}
		env = append(env, value)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
}
func executeClone(ctx context.Context, d Dependencies, argv []string, env []string) (int, error) {
	child := exec.CommandContext(ctx, "git", argv...)
	child.Env = env
	child.Stdin = d.In
	child.Stdout = d.Out
	child.Stderr = d.Err
	child.Cancel = func() error {
		if err := child.Process.Signal(os.Interrupt); err != nil {
			return child.Process.Kill()
		}
		return nil
	}
	child.WaitDelay = 2 * time.Second
	err := child.Run()
	if ctx.Err() != nil {
		return 130, ctx.Err()
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return 1, &ExitError{1, "Error: Git is required. Install git and try again."}
	}
	return 1, &ExitError{1, "Error: Could not start git clone."}
}
func newRepoClone(d Dependencies) *cobra.Command {
	var placement, branch string
	var depth int
	c := &cobra.Command{Use: "clone REPOSITORY [DIRECTORY]", Short: "Clone a repository from its nearest available Avrea mirror."}
	c.Flags().StringVar(&placement, "placement", "", "Exact mirror URL or hostname from the repository's placements.")
	c.Flags().StringVarP(&branch, "branch", "b", "", "Branch or tag to check out.")
	c.Flags().IntVar(&depth, "depth", 0, "Create a shallow clone with this many commits.")
	c.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return positionalArguments("REPOSITORY")(cmd, args)
		}
		if len(args) > 2 {
			return &ExitError{2, "Error: Got unexpected extra arguments."}
		}
		if cmd.Flags().Changed("depth") && depth < 1 {
			return &ExitError{2, "Error: Invalid value for --depth: must be at least 1."}
		}
		if len(args) > 1 {
			if info, err := os.Stat(args[1]); err == nil && !info.IsDir() {
				return &ExitError{2, "Error: Invalid value for DIRECTORY: Directory " + pythonQuote(output.CellText(args[1])) + " is a file."}
			}
		}
		return nil
	}
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		repository := args[0]
		if strings.Contains(repository, "/") {
			repository = strings.TrimSuffix(repository, ".git")
			if err := validateRepositoryName(repository); err != nil {
				return err
			}
			if strings.Count(repository, "/") != 1 {
				return &ExitError{2, "Error: Invalid value for REPOSITORY: must be an owner/repository name"}
			}
		} else if !cloneRepositoryID.MatchString(repository) {
			return &ExitError{2, "Error: Invalid value for REPOSITORY: must be an owner/repository name or an Avrea repository ID (rep-...)"}
		}
		if _, err := exec.LookPath("git"); err != nil {
			return &ExitError{1, "Error: Git is required. Install git and try again."}
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		var diagnostics sync.Mutex
		if api.Trace != nil {
			api.Trace = cloneDiagnosticWriter{api.Trace, &diagnostics}
		}
		if api.Timings != nil {
			api.Timings = cloneDiagnosticWriter{api.Timings, &diagnostics}
		}
		var pending chan struct{}
		var speculativeToken string
		var speculativeError error
		if strings.Contains(repository, "/") {
			pending = make(chan struct{})
			go func() {
				speculativeToken, _, speculativeError = issueGitToken(cmd.Context(), d, cfg, api, repository)
				close(pending)
			}()
			// Discovery/validation errors take precedence, while all HTTP work remains bounded.
			defer func() { <-pending }()
		}
		session, err := api.MirrorSession(cmd.Context(), repository)
		if err != nil {
			var he *client.HTTPError
			var te *client.TransportError
			switch {
			case errors.As(err, &he):
				return apiError(cfg, err, "discover Git mirror placements")
			case errors.As(err, &te):
				return &ExitError{1, "Error: Could not reach Avrea to discover Git mirror placements."}
			default:
				if cmd.Context().Err() != nil {
					return cmd.Context().Err()
				}
				return &ExitError{1, "Error: Avrea returned an invalid Git mirror session."}
			}
		}
		row := session.Fields
		browsable, ok := row["browsable"].(bool)
		if !ok {
			return &ExitError{1, "Error: Avrea returned an invalid Git mirror session."}
		}
		if !browsable {
			switch row["unavailable_reason"] {
			case "mirror_off":
				name := repository
				if !strings.Contains(name, "/") {
					name = "OWNER/REPO"
				}
				return &ExitError{1, "Error: Git mirroring is disabled. List locations with \x60avr repo mirror locations\x60, then run \x60avr repo mirror create " + name + " --location <cluster-id>\x60."}
			case "not_ready":
				return &ExitError{1, "Error: No Git mirror placement is ready. Check \x60avr repo mirror status\x60 and try again."}
			default:
				reason := pythonValue(row["unavailable_reason"])
				if s, ok := row["unavailable_reason"].(string); ok {
					reason = pythonQuote(s)
				}
				return &ExitError{1, "Error: Avrea reported an unknown mirror unavailability reason: " + output.CellText(reason) + "."}
			}
		}
		fullName, nameOK := row["full_name"].(string)
		rawEndpoints, endpointsOK := row["endpoints"].([]any)
		if !nameOK || !endpointsOK || len(rawEndpoints) == 0 {
			return &ExitError{1, "Error: Avrea returned an incomplete Git mirror session."}
		}
		if err = validateRepositoryName(fullName); err != nil {
			return err
		}
		var endpoints []string
		seen := map[string]bool{}
		for _, value := range rawEndpoints {
			ep, ok := value.(string)
			if !ok {
				return &ExitError{1, "Error: Avrea returned invalid Git mirror placements."}
			}
			ep, err = mirrorEndpoint(ep)
			if err != nil {
				return err
			}
			if !seen[ep] {
				endpoints = append(endpoints, ep)
				seen[ep] = true
			}
		}
		var endpoint string
		if cmd.Flags().Changed("placement") {
			endpoint, err = selectMirror(endpoints, placement)
			if err != nil {
				return err
			}
		} else if len(endpoints) == 1 {
			endpoint = endpoints[0]
		}
		token, tokenErr := "", error(nil)
		if pending != nil {
			<-pending
			if fullName == repository {
				token, tokenErr = speculativeToken, speculativeError
			}
		}
		if pending == nil || fullName != repository {
			token, _, tokenErr = issueGitToken(cmd.Context(), d, cfg, api, fullName)
		}
		if tokenErr != nil {
			return tokenErr
		}
		selectedDirectly := endpoint != ""
		if !selectedDirectly {

			if _, err = fmt.Fprintln(d.Err, "Finding the nearest available Git mirror…"); err != nil {
				return err
			}
			endpoint, err = nearestMirror(cmd.Context(), d.HTTP, endpoints, fullName, token)
			if err != nil {
				return err
			}
		}
		if _, err = fmt.Fprintf(d.Err, "Cloning %s from %s\n", output.CellText(fullName), output.CellText(endpoint)); err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return errors.New("could not locate the native CLI for the Git credential helper")
		}
		executable, err = filepath.Abs(executable)
		if err != nil {
			return err
		}
		target := cloneURL(endpoint, fullName)
		argv := []string{"-c", "http." + target + ".extraHeader=", "--config-env=http." + target + ".extraHeader=AVR_GIT_CLONE_AUTHORIZATION", "-c", "credential." + target + ".helper=", "-c", "http." + target + ".followRedirects=false", "clone"}
		for _, setting := range cloneCredentialConfig(executable, cfg.Host, target, fullName) {
			argv = append(argv, "--config", setting)
		}
		if cmd.Flags().Changed("branch") {
			argv = append(argv, "--branch", branch)
		}
		if cmd.Flags().Changed("depth") {
			argv = append(argv, "--depth", strconv.Itoa(depth))
		}
		directory := fullName[strings.LastIndex(fullName, "/")+1:]
		if len(args) > 1 {
			directory = args[1]
		}
		argv = append(argv, "--", target, directory)
		code, err := executeClone(cmd.Context(), d, argv, append(cloneEnvironment(), "AVR_GIT_CLONE_AUTHORIZATION=Authorization: Bearer "+token))
		if err != nil {
			return err
		}
		if code > 0 && selectedDirectly {
			if _, diagnostic := nearestMirror(cmd.Context(), d.HTTP, []string{endpoint}, fullName, token); diagnostic != nil {
				_, _ = fmt.Fprintln(d.Err, diagnostic)
			}
		}
		if code != 0 {
			return &ExitError{code, ""}
		}
		return nil
	})
	return c
}
