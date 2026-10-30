package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/client/wire"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// Read only the per-order credential. The vendor's environment-wide key is
// neither used nor forwarded. Errors name the operation, never file contents.
func vmSpawnOrder(d Dependencies) (string, string, error) {
	required := func(name string) (string, error) {
		value := strings.TrimSpace(d.Getenv(name))
		if value == "" {
			return "", &ExitError{2, "Error: " + name + " is not set; run this as the vendor controller's spawn script"}
		}
		return value, nil
	}
	id, err := required("CLAUDE_RUNNER_ORDER_ID")
	if err != nil {
		return "", "", err
	}
	path, err := required("CLAUDE_RUNNER_WORK_ORDER_FILE")
	if err != nil {
		return "", "", err
	}
	fail := func(err error) (string, string, error) {
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		message := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			message = "No such file or directory"
		}
		if errors.Is(err, os.ErrPermission) {
			message = "Permission denied"
		}
		return "", "", &ExitError{2, "Error: cannot read the work order file: " + message}
	}
	file, err := os.Open(path)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil {
		return fail(err)
	}
	if len(data) > 16<<20 {
		return fail(errors.New("file exceeds 16 MiB"))
	}
	if !utf8.Valid(data) {
		return fail(errors.New("file is not valid UTF-8"))
	}
	credential := strings.TrimSpace(string(data))
	if credential == "" {
		return "", "", &ExitError{2, "Error: the work order file is empty"}
	}
	return id, credential, nil
}
func createVMFromSpawn(cmd *cobra.Command, d Dependencies, o vmCreateOptions) error {
	if cmd.Flags().Changed("name") {
		return &ExitError{2, "Error: --name is derived from the work order with --from-spawn-script."}
	}
	if o.os != "" && o.os != "linux" {
		return &ExitError{2, "Error: Agent sandboxes run linux; drop --os or pass --os linux."}
	}
	if o.desktop {
		return &ExitError{2, "Error: Agent sandboxes have no remote desktop."}
	}
	if o.wait || o.json {
		return &ExitError{2, "Error: --from-spawn-script returns as soon as the sandbox is created; drop --wait and --json."}
	}
	cfg, _, api, err := d.load()
	if err != nil {
		return err
	}
	org := o.org
	if org == "" {
		org = cfg.Org
	}
	if !strings.HasPrefix(org, "org-") {
		return &ExitError{2, "Error: --from-spawn-script needs an organization ID (org-...) from --org or the default org."}
	}
	for _, flag := range []string{"repo", "ref", "fetch-depth"} {
		if cmd.Flags().Changed(flag) {
			return &ExitError{2, "Error: Claude Code clones each session's repository itself; drop --" + flag + "."}
		}
	}
	o.os = "linux"
	if o.size == "" {
		o.size = "4-vcpu"
	}
	body, rules, err := o.body(cmd)
	if err != nil {
		return err
	}
	order, credential, err := vmSpawnOrder(d)
	if err != nil {
		return err
	}
	body.IdempotencyKey.Set(order)
	bootstrap := &wire.ClaudeCodeBootstrap{Provider: wire.ClaudeCodeBootstrapProvider("claude_code"), Credential: &credential}
	if err = authenticated(cfg); err != nil {
		return err
	}
	record, err := api.CreateVM(cmd.Context(), vmBase(org), body, rules, bootstrap)
	// HTTP validation details can echo submitted values. Never print the order's
	// credential even if an upstream error accidentally includes it.
	safe := func(value string) string { return output.CellText(strings.ReplaceAll(value, credential, "[REDACTED]")) }
	if err != nil {
		var he *client.HTTPError
		if errors.As(err, &he) {
			code := 2
			if he.Status == 408 || he.Status == 409 || he.Status == 429 || he.Status >= 500 {
				code = 1
			}
			detail := he.Detail
			if detail == "" {
				detail = fmt.Sprintf("HTTP %d", he.Status)
			}
			return &ExitError{code, fmt.Sprintf("Error: agent sandbox for %s not created (HTTP %d): %s", safe(order), he.Status, safe(detail))}
		}
		var transport *client.TransportError
		if errors.As(err, &transport) {
			return &ExitError{1, "Error: agent sandbox for " + safe(order) + " not created: " + transport.Reason}
		}
		return &ExitError{1, "Error: unexpected API response while creating agent sandbox"}
	}
	vm, ok := record.Fields["vm"].(map[string]any)
	id, valid := vm["customer_vm_id"].(string)
	if !ok || !valid || id == "" {
		return &ExitError{1, "Error: unexpected API response: expected a VM ID"}
	}
	if _, err = fmt.Fprintln(d.Err, "Agent sandbox "+safe(id)+" serves "+safe(order)); err != nil {
		return err
	}
	_, err = fmt.Fprintln(d.Out, safe(id))
	return err
}
