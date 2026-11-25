package commands

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var groupLimitFields = []string{"group_id", "limit_key", "kind", "limit_type", "value", "period", "timezone", "inherited"}

var groupLimitSetFields = append(groupLimitFields[:len(groupLimitFields):len(groupLimitFields)], "previous_value")

var groupLimitClearFields = []string{"group_id", "limit_key", "cleared"}

var groupUsageFields = []string{"group_id", "group_state", "used_vcpu", "measured_at", "pools"}

// groupVCPULimit is the only limit the API enforces: a ceiling on the vCPU the
// group and its subgroups use at once, shared as one pool.
const groupVCPULimit = "max_vcpu"

func newGroupLimit(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "limit", Aliases: []string{"limits"}, Short: "Show, set and clear a group's vCPU limit."}
	c.AddCommand(newGroupLimitList(d), newGroupLimitSet(d), newGroupLimitClear(d))
	return c
}

func newGroupLimitList(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "list GROUP", Short: "Show the limits that apply to a group, its own and inherited.", Args: positionalArguments("GROUP")}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, groupLimitFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Reading group limits", "read group limits")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		page, err := api.DataList(cmd.Context(), groupRoute(id, group.id, "limits"), nil)
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{action: "read the group's limits", notFound: groupHiddenHint})
		}
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, page.Items, groupLimitFields)
		}
		cells := make([][]string, 0, len(page.Items))
		for _, row := range page.Items {
			from := "this group"
			if row["inherited"] == true {
				from = fieldText(row, "group_id")
			}
			cells = append(cells, []string{fieldText(row, "limit_key"), pythonValue(row["value"]), from})
		}
		if !d.IsTerminal() {
			return output.Table(d.Out, []string{"limit_key", "value", "set_on"}, cells, true)
		}
		if len(cells) == 0 {
			_, err = fmt.Fprintln(d.Out, "No limits apply to this group.")
			return err
		}
		return output.Grid(d.Out, []string{"Limit", "Value", "Set on"}, cells)
	})
	return c
}

func newGroupLimitSet(d Dependencies) *cobra.Command {
	var org string
	var vcpu int64
	var yes bool
	var o jsonOptions
	c := &cobra.Command{Use: "set GROUP", Short: "Set a group's own vCPU limit.", Args: positionalArguments("GROUP")}
	f := c.Flags()
	orgFlag(c, &org)
	f.Int64Var(&vcpu, "max-vcpu", 0, "The most vCPU the group and its subgroups may use at once.")
	f.BoolVarP(&yes, "yes", "y", false, "Set the limit without asking.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		if !f.Changed("max-vcpu") {
			return &ExitError{2, "Error: Missing option '--max-vcpu'."}
		}
		if vcpu < 0 {
			return &ExitError{2, "Error: --max-vcpu must be 0 or more."}
		}
		if done, err := o.prepare(cmd, d, groupLimitSetFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Setting group limits", "set group limits")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		// The group's own limit decides whether this asks: a first limit, a
		// lower one, or one replacing a value avr cannot read can make new jobs
		// wait. Another writer can still change it in between: last writer
		// wins.
		page, err := api.DataList(cmd.Context(), groupRoute(id, group.id, "limits"), nil)
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{action: "read the group's limits", notFound: groupHiddenHint})
		}
		var previous any
		for _, row := range page.Items {
			if row["inherited"] != true && fieldText(row, "limit_key") == groupVCPULimit {
				previous = row["value"]
			}
		}
		name := output.CellText(group.display())
		// Exact arithmetic: the API keeps integers past float64 precision, so a
		// number is compared as the API wrote it, never rounded through float64.
		text := pythonValue(previous)
		if number, ok := previous.(json.Number); ok {
			text = number.String()
		}
		current, readable := new(big.Rat).SetString(text)
		prompt := ""
		switch {
		case previous == nil:
			prompt = fmt.Sprintf("Set a vCPU limit of %d on group %s, which has no limit of its own?", vcpu, name)
		case !readable:
			prompt = fmt.Sprintf("Replace the vCPU limit %s of group %s with %d?", output.CellText(pythonValue(previous)), name, vcpu)
		case big.NewRat(vcpu, 1).Cmp(current) < 0:
			prompt = fmt.Sprintf("Lower the vCPU limit of group %s from %s to %d?", name, pythonValue(previous), vcpu)
		}
		if prompt != "" {
			if err = confirmChange(cmd, d, yes, "setting this group limit needs confirmation", "", prompt+" Running jobs keep running; new jobs that do not fit wait."); err != nil {
				return err
			}
		}
		body := map[string]any{"kind": "shared_pool", "limit_type": "concurrent", "value": vcpu}
		record, err := api.DataRecord(cmd.Context(), http.MethodPut, groupRoute(id, group.id, "limits", groupVCPULimit), body)
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{
				action:   "set the group's limit",
				notFound: groupHiddenHint,
				unknown:  checkBeforeRetry(groupCommand("limit list", group.id, id)),
			})
		}
		record["previous_value"] = previous
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, groupLimitSetFields)
		}
		change := fmt.Sprintf("Set the vCPU limit of group %s to %s", output.CellText(group.display()), output.CellText(pythonValue(record["value"])))
		if previous != nil {
			change += " (was " + output.CellText(pythonValue(previous)) + ")"
		}
		_, err = fmt.Fprintln(d.Out, change+".")
		return err
	})
	return c
}

func newGroupLimitClear(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	var o jsonOptions
	c := &cobra.Command{Use: "clear GROUP", Short: "Remove a group's own vCPU limit.", Args: positionalArguments("GROUP")}
	orgFlag(c, &org)
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Clear without asking.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, groupLimitClearFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Clearing group limits", "clear group limits")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		prompt := "Clear the vCPU limit of group " + output.CellText(group.display()) + "? Limits set on its parent groups still apply."
		if err = confirmChange(cmd, d, yes, "clearing a group limit needs confirmation", "", prompt); err != nil {
			return err
		}
		if _, err = api.Do(cmd.Context(), http.MethodDelete, groupRoute(id, group.id, "limits", groupVCPULimit), nil); err != nil {
			return groupFailure(cfg, err, groupRefusal{
				action:   "clear the group's limit",
				notFound: groupHiddenHint,
				unknown:  checkBeforeRetry(groupCommand("limit list", group.id, id)),
			})
		}
		record := map[string]any{"group_id": group.id, "limit_key": groupVCPULimit, "cleared": true}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, groupLimitClearFields)
		}
		_, err = fmt.Fprintln(d.Out, "Cleared the vCPU limit of group "+output.CellText(group.display())+".")
		return err
	})
	return c
}

func newGroupUsage(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "usage GROUP", Short: "Show the vCPU a group uses and the limits on its path.", Args: positionalArguments("GROUP")}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, groupUsageFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Reading group usage", "read group usage")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		record, err := api.DataRecord(cmd.Context(), http.MethodGet, groupRoute(id, group.id, "usage"), nil)
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{action: "read the group's usage", notFound: groupHiddenHint})
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, groupUsageFields)
		}
		if err = output.KeyValues(d.Out, [][2]string{
			{"Group ID", fieldText(record, "group_id")},
			{"State", fieldText(record, "group_state")},
			{"vCPU in use", pythonValue(record["used_vcpu"]) + " (this group and its subgroups)"},
			{"Measured", fieldText(record, "measured_at")},
		}); err != nil {
			return err
		}
		pools := objectList(record["pools"])
		if len(pools) == 0 {
			_, err = fmt.Fprintln(d.Out, "\nNo vCPU limit applies to this group.")
			return err
		}
		cells := make([][]string, 0, len(pools))
		for _, pool := range pools {
			cells = append(cells, []string{fieldText(pool, "group_id"), fieldText(pool, "limit_key"), pythonValue(pool["value"]), pythonValue(pool["used_vcpu"])})
		}
		if _, err = fmt.Fprintln(d.Out, "\nLimits on the group's path:"); err != nil {
			return err
		}
		return output.Grid(d.Out, []string{"Set on", "Limit", "Value", "In use"}, cells)
	})
	return c
}
