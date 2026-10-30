package commands

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func NewVM(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "vm", Short: "Manage long-running customer VMs."}
	c.AddCommand(newVMList(d), newVMShow(d), newVMUsage(d))
	return c
}
func vmBase(org string) string { return "/orgs/" + url.PathEscape(org) + "/vms" }
func vmError(cfg config.Config, err error, action string) error {
	result := apiError(cfg, err, action)
	var he *client.HTTPError
	if !errors.As(err, &he) || he.Status != 404 && he.Status != 422 {
		return result
	}
	first, rest, found := strings.Cut(result.Error(), "\n")
	if found {
		rest = "\n" + rest
	}
	return errors.New(first + "\n  Hint: Run `avr vm list` to see your VMs." + rest)
}
func vmGrid(d Dependencies, rows []map[string]any, keys, labels []string) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No data found.")
		return err
	}
	cells := [][]string{}
	for _, row := range rows {
		values := []string{}
		for _, key := range keys {
			values = append(values, organizationCell(row, key))
		}
		cells = append(cells, values)
	}
	return output.Grid(d.Out, labels, cells)
}
func newVMList(d Dependencies) *cobra.Command {
	var org, state, cursor string
	var limit int
	var asJSON bool
	c := &cobra.Command{Use: "list", Short: "List the organization's VMs, newest first (deleted ones excluded).", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&state, "state", "", "Filter by lifecycle state.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Max VMs to return (1–100).")
	c.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous response.")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the VM list as JSON.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 100 {
			return &ExitError{2, "Error: --limit must be between 1 and 100."}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("cursor") && (cursor == "" || strings.TrimSpace(cursor) != cursor || utf8.RuneCountInString(cursor) > 512) {
			return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if state != "" {
			query.Set("state", state)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, err := api.VMs(cmd.Context(), vmBase(id), query)
		if err != nil {
			return apiError(cfg, err, "list VMs")
		}
		rows := rawRecords(page)
		if asJSON {
			err = rawJSON(d, rows)
		} else {
			err = vmGrid(d, rows, []string{"customer_vm_id", "display_name", "os_type", "state", "desired_state", "cpu_count", "memory_mb", "created_at"}, []string{"VM ID", "Name", "OS", "State", "Desired", "vCPU", "Mem (MB)", "Created"})
		}
		if err != nil {
			return err
		}
		if page.NextCursor != "" {
			prefix := "\nMore results available. Next page: --cursor "
			if asJSON {
				prefix = "next_cursor: "
			}
			_, err = fmt.Fprintln(d.Err, prefix+output.CellText(shellQuotePython(page.NextCursor)))
		}
		return err
	})
	return c
}
func newVMShow(d Dependencies) *cobra.Command {
	var org string
	var asJSON bool
	c := &cobra.Command{Use: "show VM_ID", Short: "Show a VM's details, including connection endpoints and egress rules.", Args: positionalArguments("VM_ID")}
	orgFlag(c, &org)
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the full VM record (with egress rules) as JSON.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.VM(cmd.Context(), vmBase(id)+"/"+url.PathEscape(args[0]))
		if err != nil {
			return vmError(cfg, err, "fetch VM")
		}
		if asJSON {
			return rawJSON(d, record.Fields)
		}
		if err = renderVM(d, record.Fields, ""); err != nil {
			return err
		}
		rules := objectList(record.Fields["egress_rules"])
		if len(rules) == 0 {
			return nil
		}
		for _, row := range rules {
			matcher := fieldText(row, "cidr")
			if matcher == "" {
				matcher = fieldText(row, "fqdn")
			}
			if matcher == "" {
				matcher = "?"
			}
			if row["is_default"] == true {
				matcher = "*"
			}
			row["matcher"] = matcher
		}
		if _, err = fmt.Fprintln(d.Out, "\nEgress rules:"); err != nil {
			return err
		}
		return vmGrid(d, rules, []string{"position", "action", "matcher", "protocol", "port_start", "port_end"}, []string{"#", "Action", "Match", "Proto", "Port start", "Port end"})
	})
	return c
}

var vmUsageDatePattern = regexp.MustCompile("^[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}(T[0-9]{1,2}:[0-9]{1,2}:[0-9]{1,2})?$")

func vmUsageDate(value, flag string) (string, error) {
	if vmUsageDatePattern.MatchString(value) {
		for _, layout := range []string{"2006-1-2", "2006-1-2T15:4:5"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.Format("2006-01-02T15:04:05"), nil
			}
		}
	}
	return "", &ExitError{2, "Error: Invalid value for '--" + flag + "': expected YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS"}
}
func newVMUsage(d Dependencies) *cobra.Command {
	var org, start, end string
	var asJSON bool
	c := &cobra.Command{Use: "usage", Short: "Show usage metering (runtime / vCPU / memory seconds) per VM.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&start, "start", "", "Inclusive period start (default: 30 days ago).")
	c.Flags().StringVar(&end, "end", "", "Exclusive period end (default: now).")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the usage report as JSON.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		query := url.Values{}
		for _, part := range []struct{ flag, key, value string }{{"start", "period_start", start}, {"end", "period_end", end}} {
			if cmd.Flags().Changed(part.flag) {
				value, err := vmUsageDate(part.value, part.flag)
				if err != nil {
					return err
				}
				query.Set(part.key, value)
			}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.VMUsage(cmd.Context(), vmBase(id)+"/usage", query)
		if err != nil {
			return apiError(cfg, err, "fetch VM usage")
		}
		data := record.Fields
		if asJSON {
			return rawJSON(d, data)
		}
		if err = vmGrid(d, objectList(data["vms"]), []string{"customer_vm_id", "display_name", "os_type", "state", "run_count", "runtime_seconds", "vcpu_seconds", "memory_mb_seconds"}, []string{"VM ID", "Name", "OS", "State", "Runs", "Runtime (s)", "vCPU (s)", "Mem (MB-s)"}); err != nil {
			return err
		}
		if _, err = fmt.Fprintln(d.Out); err != nil {
			return err
		}
		return output.KeyValues(d.Out, [][2]string{{"Period start", pythonValue(data["period_start"])}, {"Period end", pythonValue(data["period_end"])}, {"Total runtime (s)", pythonValue(data["total_runtime_seconds"])}, {"Total vCPU (s)", pythonValue(data["total_vcpu_seconds"])}, {"Total memory (MB-s)", pythonValue(data["total_memory_mb_seconds"])}})
	})
	return c
}
