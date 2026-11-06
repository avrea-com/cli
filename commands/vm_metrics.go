package commands

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newVMMetrics(d Dependencies) *cobra.Command {
	var org string
	var sources []string
	var start, end int64
	var watch, asJSON bool
	c := &cobra.Command{Use: "metrics VM_ID_OR_NAME", Short: "Show CPU/memory/IO metrics for a VM's current or last run.", Args: positionalArguments("VM_ID_OR_NAME")}
	orgFlag(c, &org)
	f := c.Flags()
	f.StringArrayVar(&sources, "source", nil, "Metric source. Repeatable; defaults to cpu and memory.")
	f.Int64Var(&start, "start", 0, "Start time in Unix seconds (single-shot only).")
	f.Int64Var(&end, "end", 0, "End time in Unix seconds (single-shot only).")
	f.BoolVarP(&watch, "watch", "w", false, "Stream live metrics until the run completes; Ctrl-C to exit.")
	f.BoolVar(&asJSON, "json", false, "Emit metrics JSON; watch emits one snapshot per line.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		selected, err := selectMetricSources(cmd, sources, watch)
		if err != nil {
			return err
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		path := vmBase(orgID) + "/" + url.PathEscape(args[0])
		record, err := api.VM(cmd.Context(), path)
		if err != nil {
			return vmError(cfg, err, "get VM")
		}
		vm := record
		ram := metricNumber(vm["memory_mb"]) * (1 << 20)
		render := func(metrics map[string]map[string]any, live bool, note, message string) error {
			if watch {
				if err := clearMetricFrame(d); err != nil {
					return err
				}
			}
			if err := output.KeyValues(d.Out, [][2]string{{"VM", output.CellText(fieldText(vm, "customer_vm_id"))}, {"Name", output.CellText(pythonValue(vm["display_name"]))}}); err != nil {
				return err
			}
			if note != "" {
				if _, err := fmt.Fprintln(d.Out, note); err != nil {
					return err
				}
			} else if len(metrics) == 0 {
				if _, err := fmt.Fprintln(d.Out, "No metrics available for this run."); err != nil {
					return err
				}
			}
			for _, source := range selected {
				if _, err := fmt.Fprintln(d.Out, renderMetricGauge(d, source, metrics[source], ram, live)); err != nil {
					return err
				}
			}
			if message != "" {
				_, err := fmt.Fprintln(d.Out, "\n"+message)
				return err
			}
			return nil
		}
		if !watch {
			query := url.Values{"source": selected}
			if f.Changed("start") {
				query.Set("start", strconv.FormatInt(start, 10))
			}
			if f.Changed("end") {
				query.Set("end", strconv.FormatInt(end, 10))
			}
			record, err := api.VMMetrics(cmd.Context(), path+"/metrics", query)
			if err != nil {
				return apiError(cfg, err, "get VM metrics")
			}
			metrics, err := filterMetricSnapshot(record["metrics"], selected)
			if err != nil {
				return err
			}
			if asJSON {
				return rawJSON(d, metrics)
			}
			return render(metrics, false, "", "")
		}
		drawn := false
		return watchMetrics(cmd.Context(), d, api, vmBase(orgID)+"/"+url.PathEscape(fieldText(vm, "customer_vm_id"))+"/observability/stream", selected, asJSON, func(u metricUpdate) error {
			var err error
			if u.complete {
				if !u.sawBundle {
					err = render(u.metrics, false, "", u.message)
				}
			} else if u.changed || !drawn {
				note := ""
				if !u.sawBundle {
					note = "Waiting for metrics..."
				}
				err = render(u.metrics, u.running, note, u.message)
			}
			drawn = true
			return err
		})
	})
	return c
}
