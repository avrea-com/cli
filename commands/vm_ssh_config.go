package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func vmSSHEndpoint(ctx context.Context, cfg config.Config, api *client.Client, org, id string) (map[string]any, error) {
	record, err := api.VM(ctx, vmBase(org)+"/"+url.PathEscape(id))
	if err != nil {
		return nil, vmError(cfg, err, "fetch VM")
	}
	endpoints, _ := record.Fields["endpoints"].(map[string]any)
	endpoint, _ := endpoints["ssh"].(map[string]any)
	if len(endpoint) == 0 {
		return nil, vmMissingSSHEndpoint(record.Fields, id)
	}
	return endpoint, nil
}
func vmSSHAddress(ep map[string]any) (string, string, int, error) {
	host := strings.TrimSpace(fieldText(ep, "external_ip"))
	user := strings.TrimSpace(fieldText(ep, "username"))
	if user == "" {
		user = "runner"
	}
	for _, item := range []struct{ value, label string }{{host, "IP"}, {user, "username"}} {
		if strings.ContainsAny(item.value, "\r\n") {
			return "", "", 0, &ExitError{1, "Error: VM endpoint " + item.label + " contains an embedded newline; refusing to write it."}
		}
	}
	port, err := strconv.Atoi(fieldText(ep, "external_port"))
	if err != nil || port < 1 || port > 65535 {
		return "", "", 0, &ExitError{1, "Error: VM endpoint returned an invalid SSH port: " + pythonValue(ep["external_port"]) + "."}
	}
	if host == "" || !sshHost.MatchString(host) || strings.HasPrefix(host, "-") || !sshUser.MatchString(user) || strings.HasPrefix(user, "-") {
		return "", "", 0, &ExitError{1, "Error: VM endpoint returned an invalid SSH host or username."}
	}
	return host, user, port, nil
}
func vmKnownHostsLine(key, host string, port int) (string, error) {
	key = strings.TrimSpace(key)
	if strings.ContainsAny(key, "\r\n") {
		return "", &ExitError{1, "Error: VM endpoint returned a malformed SSH host key (multiple lines); refusing to pin it."}
	}
	if port != 22 {
		host = "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + " " + key + "\n", nil
}

// Atomic replacement retains unrelated SSH settings and publishes only private,
// complete files. Existing parent directory permissions are not changed.
func vmAtomicText(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = io.WriteString(file, content); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func vmReadText(path string) (string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil {
		return "", err
	}
	if len(raw) > 16<<20 {
		return "", errors.New("SSH file exceeds 16 MiB")
	}
	if !utf8.Valid(raw) {
		return "", errors.New("SSH file is not valid UTF-8")
	}
	// Python read_text uses universal newline handling.
	return strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n"), nil
}
func vmUpsertKnownHosts(path, line string) error {
	text, err := vmReadText(path)
	if err != nil {
		return err
	}
	host, _, _ := strings.Cut(line, " ")
	kept := ""
	for _, old := range strings.SplitAfter(text, "\n") {
		if !strings.HasPrefix(old, host+" ") {
			kept += old
		}
	}
	if kept != "" && !strings.HasSuffix(kept, "\n") {
		kept += "\n"
	}
	return vmAtomicText(path, kept+line)
}
func vmSSHConfigPath(value, label string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", &ExitError{1, "Error: SSH config " + label + " contains an embedded newline; refusing to write it."}
	}
	if strings.Contains(value, "\"") {
		return "", &ExitError{1, "Error: SSH config " + label + " contains a double quote, which ssh_config cannot escape; refusing to write it."}
	}
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "\"" + value + "\"", nil
	}
	return value, nil
}
func vmSSHConfigBlock(alias, host, user string, port int, identity, known string) (string, error) {
	lines := []string{"Host " + alias, "    HostName " + host, "    User " + user, "    Port " + strconv.Itoa(port)}
	for _, item := range []struct{ value, label string }{{identity, "IdentityFile"}, {known, "UserKnownHostsFile"}} {
		if item.value != "" {
			value, err := vmSSHConfigPath(item.value, item.label)
			if err != nil {
				return "", err
			}
			lines = append(lines, "    "+item.label+" "+value)
		}
	}
	strict := "accept-new"
	if known != "" {
		strict = "yes"
	}
	lines = append(lines, "    StrictHostKeyChecking "+strict)
	return strings.Join(lines, "\n") + "\n", nil
}
func vmUpsertSSHBlock(path, alias, block string) (string, error) {
	text, err := vmReadText(path)
	if err != nil {
		return "", err
	}
	begin, end := "# >>> avrea vm "+alias+" >>>", "# <<< avrea vm "+alias+" <<<"
	managed := []string{begin}
	managed = append(managed, strings.Split(strings.TrimRight(block, "\n"), "\n")...)
	managed = append(managed, end)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	kept := []string{}
	skipping := false
	at := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == begin {
			skipping = true
			at = len(kept)
			continue
		}
		if skipping {
			skipping = strings.TrimSpace(line) != end
			continue
		}
		kept = append(kept, line)
	}
	if skipping {
		return "", &ExitError{1, fmt.Sprintf("Error: %s has an unterminated '%s' block (missing its '%s' marker); fix it by hand and re-run.", path, begin, end)}
	}
	action := "added"
	if at >= 0 {
		action = "updated"
		tail := append([]string{}, kept[at:]...)
		kept = append(kept[:at], managed...)
		kept = append(kept, tail...)
	} else {
		for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
			kept = kept[:len(kept)-1]
		}
		if len(kept) > 0 {
			kept = append(kept, "")
		}
		kept = append(kept, managed...)
	}
	return action, vmAtomicText(path, strings.Join(kept, "\n")+"\n")
}
func vmPinningOverrides(path string) ([]string, error) {
	text, err := vmReadText(path)
	if err != nil {
		return nil, err
	}
	found := []string{}
	seen := map[string]bool{}
	catchall := false
	watched := map[string]string{"userknownhostsfile": "UserKnownHostsFile", "stricthostkeychecking": "StrictHostKeyChecking"}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tokens := strings.Fields(strings.Replace(line, "=", " ", 1))
		if len(tokens) == 0 {
			continue
		}
		keyword := strings.ToLower(tokens[0])
		if keyword == "host" || keyword == "match" {
			catchall = false
			if keyword == "host" {
				for _, token := range tokens[1:] {
					if token == "*" {
						catchall = true
					}
				}
			}
			continue
		}
		if label, ok := watched[keyword]; ok && catchall && !seen[label] {
			found = append(found, label)
			seen[label] = true
		}
	}
	return found, nil
}
func newVMSSHConfig(d Dependencies) *cobra.Command {
	var org, identity, alias, known, configPath string
	var appendBlock bool
	c := &cobra.Command{Use: "ssh-config VM_ID", Short: "Print or append an SSH config block for a running VM.", Args: positionalArguments("VM_ID")}
	orgFlag(c, &org)
	f := c.Flags()
	f.StringVarP(&identity, "identity", "i", "", "IdentityFile to write into the block.")
	f.StringVar(&alias, "host-alias", "", "Host alias; defaults to avr-<vm-id>.")
	f.StringVar(&known, "known-hosts-file", "", "Host-key file; defaults to ~/.ssh/avr_known_hosts.")
	f.BoolVar(&appendBlock, "append", false, "Add or replace the managed block in your SSH config.")
	f.StringVar(&configPath, "config-file", "", "SSH config file for --append; defaults to ~/.ssh/config.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if configPath != "" && !appendBlock {
			return &ExitError{2, "Error: --config-file requires --append."}
		}
		if alias == "" {
			alias = "avr-" + args[0]
		}
		if !sshUser.MatchString(alias) {
			return &ExitError{2, "Error: --host-alias '" + output.CellText(alias) + "' is not valid; use letters, digits, '.', '_' or '-' (no whitespace)."}
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		ep, err := vmSSHEndpoint(cmd.Context(), cfg, api, orgID, args[0])
		if err != nil {
			return err
		}
		host, user, port, err := vmSSHAddress(ep)
		if err != nil {
			return err
		}
		knownPath := ""
		if key := fieldText(ep, "host_key"); key != "" {
			value := known
			if value == "" {
				value = "~/.ssh/avr_known_hosts"
			}
			knownPath, err = expandArtifactPath(value)
			if err != nil {
				return err
			}
			knownPath = filepath.FromSlash(knownPath)
			line, err := vmKnownHostsLine(key, host, port)
			if err != nil {
				return err
			}
			if err = vmUpsertKnownHosts(knownPath, line); err != nil {
				return fmt.Errorf("write SSH host key: %w", err)
			}
			if _, err = fmt.Fprintf(d.Err, "Pinned host key for %s in %s\n", output.CellText(alias), output.CellText(knownPath)); err != nil {
				return err
			}
		} else {
			if _, err = fmt.Fprintln(d.Err, "Warning: this VM's endpoint has no SSH host key; the block uses accept-new (trust-on-first-use) instead of pinning."); err != nil {
				return err
			}
		}
		block, err := vmSSHConfigBlock(alias, host, user, port, identity, knownPath)
		if err != nil {
			return err
		}
		if !appendBlock {
			_, err = io.WriteString(d.Out, block)
			return err
		}
		if configPath == "" {
			configPath = "~/.ssh/config"
		}
		path, err := expandArtifactPath(configPath)
		if err != nil {
			return err
		}
		path = filepath.FromSlash(path)
		action, err := vmUpsertSSHBlock(path, alias, block)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(d.Err, "%s host %s in %s. Connect with: ssh %s\n", strings.ToUpper(action[:1])+action[1:], output.CellText(alias), output.CellText(path), output.CellText(alias)); err != nil {
			return err
		}
		if action == "added" && knownPath != "" {
			overrides, err := vmPinningOverrides(path)
			if err != nil {
				return err
			}
			if len(overrides) > 0 {
				_, err = fmt.Fprintf(d.Err, "Warning: an earlier 'Host *' block sets %s; ssh uses the first match, so it may override the pinning appended for %s.\n", strings.Join(overrides, " and "), output.CellText(alias))
				return err
			}
		}
		return nil
	})
	return c
}
