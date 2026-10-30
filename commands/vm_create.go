package commands

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/avrea-com/cli/client/wire"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var vmBranchName = regexp.MustCompile(`^[A-Za-z0-9_](?:[A-Za-z0-9._/-]*[A-Za-z0-9_-])?$`)
var vmCommitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
var vmCacheAlias = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var vmCacheKey = regexp.MustCompile(`^cache\.[a-z0-9][a-z0-9-]*\.enabled$`)

func validateVMBranch(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || vmCommitSHA.MatchString(value) || strings.HasPrefix(value, "refs/") || strings.Contains(value, "..") || !vmBranchName.MatchString(value) {
		return "", &ExitError{2, "Error: --ref '" + output.CellText(value) + "' is not a branch name; tags, pull-request refs, and raw commit SHAs are not supported."}
	}
	return value, nil
}
func vmCacheName(key string) string {
	if strings.HasPrefix(key, "cache.") && strings.HasSuffix(key, ".enabled") {
		return strings.TrimSuffix(strings.TrimPrefix(key, "cache."), ".enabled")
	}
	return key
}
func vmDisabledCaches(values []string) (map[string]wire.CustomerVmCreateRequestCacheSettingOverrides, error) {
	result := map[string]wire.CustomerVmCreateRequestCacheSettingOverrides{}
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			token = strings.ToLower(strings.TrimSpace(token))
			if token == "" {
				continue
			}
			switch {
			case vmCacheKey.MatchString(token):
				result[token] = false
			case vmCacheAlias.MatchString(token):
				result["cache."+token+".enabled"] = false
			default:
				return nil, &ExitError{2, "Error: --disable-cache '" + output.CellText(token) + "' is not a cache name; use e.g. 'gha' or 'cache.gha.enabled'."}
			}
		}
	}
	return result, nil
}

type vmCreateOptions struct {
	org, name, os, version, size, ttl, egress, repo, ref, spawn string
	keys, caches                                                []string
	desktop, ephemeral, wait, json                              bool
	depth, timeout                                              int
}

func newVMCreate(d Dependencies) *cobra.Command {
	o := vmCreateOptions{}
	c := &cobra.Command{Use: "create", Short: "Create a long-running VM.", Args: positionalArguments()}
	f := c.Flags()
	orgFlag(c, &o.org)
	f.StringVar(&o.name, "name", "", "Human-readable VM name. Required unless --from-spawn-script.")
	f.StringVar(&o.os, "os", "", "Guest OS: linux, macos, or windows.")
	f.StringVar(&o.version, "os-version", "", "Guest OS version; defaults to the latest for the chosen OS.")
	f.StringVar(&o.size, "size", "", "Hardware tier: 1-vcpu, 2-vcpu, 4-vcpu, 8-vcpu, 16-vcpu, 32-vcpu.")
	f.StringArrayVar(&o.keys, "ssh-key", nil, "SSH public key or @path; repeatable.")
	f.BoolVar(&o.desktop, "remote-desktop", false, "Enable RDP or VNC, depending on the guest OS.")
	f.Var(inverseBool{&o.desktop}, "no-remote-desktop", "Disable remote desktop.")
	f.Lookup("no-remote-desktop").NoOptDefVal = "true"
	f.StringVar(&o.ttl, "ttl", "", "Auto-stop duration, e.g. 8h, 7d, or 1800s. Defaults to 8h.")
	f.StringVar(&o.egress, "egress-rules", "", "Per-VM egress rules as a JSON array or @path.")
	f.StringVar(&o.repo, "repo", "", "Git repository to preload (owner/repo).")
	f.StringVar(&o.ref, "ref", "", "Branch to preload; requires --repo.")
	f.IntVar(&o.depth, "fetch-depth", 0, "History to preload; 0 means all branches and tags.")
	f.StringArrayVar(&o.caches, "disable-cache", nil, "Disable caches; repeatable or comma-separated.")
	f.BoolVar(&o.ephemeral, "ephemeral", false, "Acknowledge that the VM's disk is ephemeral.")
	f.BoolVar(&o.wait, "wait", false, "Wait until RUNNING and connection endpoints are ready.")
	f.IntVar(&o.timeout, "wait-timeout", 300, "Seconds to wait with --wait.")
	f.BoolVar(&o.json, "json", false, "Emit the VM and one-time password as JSON.")
	f.StringVar(&o.spawn, "from-spawn-script", "", "Vendor spawn script mode: claude-code.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		for _, item := range []struct {
			flag, value string
			choices     []string
		}{
			{"os", o.os, []string{"linux", "macos", "windows"}},
			{"os-version", o.version, []string{"ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04", "macos-26", "windows-2025"}},
			{"size", o.size, []string{"1-vcpu", "2-vcpu", "4-vcpu", "8-vcpu", "16-vcpu", "32-vcpu"}},
			{"from-spawn-script", o.spawn, []string{"claude-code"}},
		} {
			if f.Changed(item.flag) {
				if err := validateFirewallChoice(item.flag, item.value, item.choices); err != nil {
					return err
				}
			}
		}
		if f.Changed("fetch-depth") && o.depth < 0 {
			return &ExitError{2, "Error: Invalid value for '--fetch-depth': must be at least 0."}
		}
		if f.Changed("from-spawn-script") {
			return createVMFromSpawn(cmd, d, o)
		}
		for _, flag := range []string{"name", "os", "size"} {
			if !f.Changed(flag) {
				return &ExitError{2, "Error: Missing option '" + "--" + flag + "'."}
			}
		}
		if !o.ephemeral {
			return &ExitError{2, "Error: These VMs have no persistent storage yet: stopping a VM (or losing its node) discards the disk, and a restart boots fresh from the image. Pass --ephemeral to acknowledge."}
		}
		if o.ref != "" && o.repo == "" {
			return &ExitError{2, "Error: --ref requires --repo."}
		}
		if f.Changed("fetch-depth") && o.repo == "" {
			return &ExitError{2, "Error: --fetch-depth requires --repo."}
		}
		if o.ref != "" {
			ref, err := validateVMBranch(o.ref)
			if err != nil {
				return err
			}
			o.ref = ref
		}
		body, rules, err := o.body(cmd)
		if err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, o.org)
		if err != nil {
			return err
		}
		record, err := api.CreateVM(cmd.Context(), vmBase(id), body, rules, nil)
		if err != nil {
			return apiError(cfg, err, "create VM")
		}
		data := record.Fields
		vm, ok := data["vm"].(map[string]any)
		if !ok {
			return &ExitError{1, "Error: unexpected API response: expected a VM record"}
		}
		password := fieldText(data, "password")
		vmID := billingText(vm, "customer_vm_id", "None")
		notes := []string{}
		if note := fieldText(data, "precheckout_note"); note != "" {
			notes = append(notes, "Note: "+note)
		}
		if body.CacheSettingOverrides != nil {
			aliases := []string{}
			for key := range *body.CacheSettingOverrides {
				aliases = append(aliases, vmCacheName(key))
			}
			sort.Strings(aliases)
			notes = append(notes, "Caches disabled: "+strings.Join(aliases, ", "))
		}
		if o.wait {
			if !o.json {
				if err = vmPrintPassword(d, password); err != nil {
					return err
				}
				for _, note := range notes {
					if _, err = fmt.Fprintln(d.Err, d.errorPaint(output.CellText(note), "33")); err != nil {
						return err
					}
				}
				if _, err = fmt.Fprintln(d.Out); err != nil {
					return err
				}
			}
			if _, err = fmt.Fprintf(d.Err, "Waiting up to %ds for %s to become RUNNING...\n", o.timeout, output.CellText(vmID)); err != nil {
				return err
			}
			final, disposition, err := waitForVM(cmd.Context(), d, cfg, api, vmBase(id)+"/"+url.PathEscape(vmID), o.timeout, vmConnectable, false, nil)
			if err != nil {
				return err
			}
			shown := final
			if len(shown) == 0 {
				shown = vm
			}
			if o.json {
				return vmWaitJSON(d, shown, data["password"], disposition)
			}
			if _, err = fmt.Fprintln(d.Out); err != nil {
				return err
			}
			if err = renderVM(d, shown, password); err != nil {
				return err
			}
			return vmWaitExit(d, final, disposition, "connectable", o.timeout, vmID)
		}
		if o.json {
			return rawJSON(d, data)
		}
		if err = renderVM(d, vm, password); err != nil {
			return err
		}
		if err = vmPrintPassword(d, password); err != nil {
			return err
		}
		if _, err = fmt.Fprintln(d.Out); err != nil {
			return err
		}
		for _, note := range notes {
			if _, err = fmt.Fprintln(d.Err, d.errorPaint(output.CellText(note), "33")); err != nil {
				return err
			}
			if _, err = fmt.Fprintln(d.Out); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(d.Out, "Provisioning started. Poll status with: avr vm show "+output.CellText(vmID))
		return err
	})
	return c
}
func (o vmCreateOptions) body(cmd *cobra.Command) (wire.CustomerVmCreateRequest, *[]any, error) {
	body := wire.CustomerVmCreateRequest{OsType: wire.OsType(o.os), Size: wire.VmSize(o.size), EnableRemoteDesktop: &o.desktop}
	body.Ephemeral.Set(true)
	ttl := 28800
	var err error
	if cmd.Flags().Changed("ttl") {
		ttl, err = parseVMTTL(o.ttl)
		if err != nil {
			return body, nil, err
		}
	}
	body.TtlSeconds = &ttl
	keys, err := resolveVMSSHKeys(o.keys)
	if err != nil {
		return body, nil, err
	}
	body.SshPublicKeys = &keys
	var rules *[]any
	if cmd.Flags().Changed("egress-rules") {
		value, err := parseVMEgress(o.egress)
		if err != nil {
			return body, nil, err
		}
		rules = &value
	}
	caches, err := vmDisabledCaches(o.caches)
	if err != nil {
		return body, nil, err
	}
	if len(caches) > 0 {
		body.CacheSettingOverrides = &caches
	}
	if cmd.Flags().Changed("name") {
		body.DisplayName.Set(o.name)
	}
	if cmd.Flags().Changed("os-version") {
		body.OsVersion.Set(wire.OsVersion(o.version))
	}
	if cmd.Flags().Changed("repo") {
		body.Repo.Set(o.repo)
	}
	if cmd.Flags().Changed("ref") {
		body.Ref.Set(o.ref)
	}
	if cmd.Flags().Changed("fetch-depth") {
		body.FetchDepth.Set(o.depth)
	}
	return body, rules, nil
}
