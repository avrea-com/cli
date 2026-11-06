package commands

import (
	"fmt"
	"maps"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newVMCapabilities(d Dependencies) *cobra.Command {
	var org string
	var asJSON bool
	c := &cobra.Command{Use: "capabilities", Short: "List available operating systems, architectures, sizes, and quotas.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().BoolVar(&asJSON, "json", false, "Emit capabilities and quotas as JSON.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.VMCapabilities(cmd.Context(), vmBase(orgID)+"/capabilities")
		if err != nil {
			return apiError(cfg, err, "get VM capabilities")
		}
		data := record
		if asJSON {
			return rawJSON(d, data)
		}
		value := func(key string, v any) string {
			if v != nil {
				return pythonValue(v)
			}
			if strings.HasPrefix(key, "max_") {
				return "unlimited"
			}
			return "n/a"
		}
		pairs := [][2]string{}
		for _, key := range []string{"max_active_vcpus", "active_vcpus", "max_active_per_user", "active_count"} {
			pairs = append(pairs, [2]string{strings.ReplaceAll(key, "_", " "), value(key, data[key])})
		}
		if err = output.KeyValues(d.Out, pairs); err != nil {
			return err
		}
		rows := []map[string]any{}
		for _, os := range objectList(data["os_types"]) {
			var text string
			if os["max_active"] == nil {
				text = fmt.Sprintf("%s: no per-OS limit", pythonValue(os["os_type"]))
			} else {
				text = fmt.Sprintf("%s: active %s, limit %s", pythonValue(os["os_type"]), value("active_count", os["active_count"]), pythonValue(os["max_active"]))
			}
			if _, err = fmt.Fprintln(d.Out, "\n"+output.CellText(text)); err != nil {
				return err
			}
			for _, arch := range objectList(os["architectures"]) {
				versions := []string{}
				rawVersions, _ := arch["versions"].([]any)
				for _, v := range rawVersions {
					versions = append(versions, pythonValue(v))
				}
				for _, spec := range objectList(arch["size_specs"]) {
					row := map[string]any{"os": os["os_type"], "arch": arch["architecture"], "versions": strings.Join(versions, ", "), "default": arch["default_version"]}
					maps.Copy(row, spec)
					rows = append(rows, row)
				}
			}
		}
		return vmGrid(d, rows, []string{"os", "arch", "versions", "default", "size", "cpu_count", "memory_mb", "disk_gb"}, []string{"OS", "Arch", "Versions", "Default", "Size", "vCPU", "Mem (MB)", "Disk (GB)"})
	})
	return c
}
