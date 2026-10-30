package commands

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
	"golang.org/x/net/idna"
)

var repoPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var dnsLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func gitURL(raw, repo string) (*url.URL, error) {
	parts := strings.Split(repo, "/")
	if len(parts) < 2 {
		return nil, &ExitError{2, "Error: REPOSITORY must be an owner/repository name"}
	}
	for _, part := range parts {
		if part == "." || part == ".." || !repoPart.MatchString(part) {
			return nil, &ExitError{2, "Error: REPOSITORY must be an owner/repository name"}
		}
	}
	u, err := url.Parse(raw)
	invalid := errors.New("invalid repository URL for the Git credential helper")
	if err != nil || config.ValidateHost(raw) != nil || u.User != nil || strings.ContainsAny(raw, "\\?#") || u.Path != "/"+repo+".git" || u.RawPath != "" {
		return nil, invalid
	}
	host := strings.ToLower(u.Hostname())
	authority := host
	if strings.Contains(host, ":") {
		if strings.Contains(host, "%") || net.ParseIP(host) == nil {
			return nil, invalid
		}
		authority = "[" + host + "]"
	} else {
		if len(host) > 253 {
			return nil, invalid
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if !dnsLabel.MatchString(label) {
				return nil, invalid
			}
		}
		if _, err := idna.Lookup.ToUnicode(host); err != nil {
			return nil, &ExitError{1, "Error: The API returned an invalid or insecure mirror endpoint."}
		}
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil {
			return nil, invalid
		}
		authority += ":" + strconv.Itoa(n)
	}
	if strings.ToLower(u.Host) != authority {
		return nil, invalid
	}
	return u, nil
}

func newGitCredential(d Dependencies) *cobra.Command {
	var rawURL, repo string
	cmd := &cobra.Command{Use: "git-credential <get|store|erase>", Short: "Git credential protocol adapter.", Hidden: true, Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&rawURL, "url", "", "Exact clone URL this helper may authenticate.")
	cmd.Flags().StringVar(&repo, "repo", "", "Repository whose Git token to request.")
	cmd.RunE = run(func(cmd *cobra.Command, args []string) error {
		if rawURL == "" || repo == "" {
			return &ExitError{2, "Error: --url and --repo are required"}
		}
		switch args[0] {
		case "store", "erase":
			return nil
		case "get":
		default:
			return &ExitError{2, "Error: operation must be get, store, or erase"}
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		u, err := gitURL(rawURL, repo)
		if err != nil {
			return err
		}
		fields := map[string]string{}
		bearer := false
		scanner := bufio.NewScanner(d.In)
		scanner.Buffer(make([]byte, 4096), 65537)
		// Scanner's default line splitter strips CR, which this protocol must reject.
		scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
			for i, b := range data {
				if b == '\n' {
					return i + 1, data[:i], nil
				}
			}
			if atEOF && len(data) > 0 {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				break
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || len(line) > 65535 || strings.ContainsAny(line, "\r\x00") {
				return errors.New("Invalid Git credential request.") //nolint:staticcheck // Preserve the existing user-facing CLI diagnostic.
			}
			if key == "capability[]" {
				bearer = bearer || value == "authtype"
			} else {
				fields[key] = value
			}
		}
		if scanner.Err() != nil {
			return errors.New("Invalid Git credential request.") //nolint:staticcheck // Preserve the existing user-facing CLI diagnostic.
		}
		if fields["protocol"] != u.Scheme || fields["host"] != u.Host || fields["path"] != strings.TrimPrefix(u.Path, "/") {
			return nil
		}
		fail := func(err error) error { _, _ = fmt.Fprint(d.Out, "quit=1\n\n"); return err }
		if err := authenticated(cfg); err != nil {
			return fail(err)
		}
		token, expires, err := issueGitToken(cmd.Context(), d, cfg, api, repo)
		if err != nil {
			return fail(err)
		}
		if bearer {
			_, err = fmt.Fprintf(d.Out, "capability[]=authtype\nauthtype=Bearer\ncredential=%s\nephemeral=true\n\n", token)
		} else {
			_, err = fmt.Fprintf(d.Out, "username=x-access-token\npassword=%s\npassword_expiry_utc=%d\n\n", token, expires.Unix())
		}
		return err
	})
	return cmd
}
