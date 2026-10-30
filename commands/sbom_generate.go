package commands

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var sbomGenerateFields = []string{"ai_task_id", "status", "task_status", "error_code", "commit_sha", "recorded_at"}

func fullSBOMCommit(ref string) bool {
	return (len(ref) == 40 || len(ref) == 64) && sbomCommitPattern.MatchString(ref)
}

func newSBOMGenerate(d Dependencies) *cobra.Command {
	var org, repo, refOption string
	var wait bool
	var timeout int
	var o jsonOptions
	c := &cobra.Command{Use: "generate [REF]", Short: "Start SBOM generation for a repository.", Args: cobra.MaximumNArgs(1)}
	cacheScopeFlags(c, &org, &repo)
	o.flags(c)
	f := c.Flags()
	f.StringVar(&refOption, "ref", "", "Same as the REF argument.")
	f.BoolVar(&wait, "wait", false, "Wait until generation finishes before returning.")
	f.IntVar(&timeout, "wait-timeout", 900, "Seconds to wait when --wait is set.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		var ref string
		if len(args) > 0 {
			ref = strings.TrimSpace(args[0])
			if ref == "" {
				return &ExitError{2, "Error: REF must not be empty."}
			}
		}
		option := strings.TrimSpace(refOption)
		if f.Changed("ref") && option == "" {
			return &ExitError{2, "Error: --ref must not be empty."}
		}
		if timeout < 1 || int64(timeout) > int64((1<<63-1)/time.Second) {
			return &ExitError{2, "Error: --wait-timeout must be at least 1 second and fit a duration."}
		}
		if done, err := o.prepare(cmd, d, sbomGenerateFields); done || err != nil {
			return err
		}
		if ref != "" && option != "" && ref != option {
			return &ExitError{2, "Error: Pass the ref either as the REF argument or with --ref, not both."}
		}
		if ref == "" {
			ref = option
		}
		cfg, api, id, rid, err := sbomContext(cmd.Context(), d, org, repo)
		if err != nil {
			return err
		}
		base := sbomBase(id, rid)
		sha := ""
		if fullSBOMCommit(strings.ToLower(ref)) {
			sha = strings.ToLower(ref)
		}
		var baseline map[string]any
		if wait && sha != "" {
			record, e := api.SBOM(cmd.Context(), base+"/commits/"+sha)
			if e != nil {
				var he *client.HTTPError
				if !errors.As(e, &he) || he.Status != 404 {
					return sbomError(cfg, e, "get SBOM", false)
				}
			} else {
				baseline = record.Fields
			}
		}
		deadline := d.Now().Add(time.Duration(timeout) * time.Second)
		body := map[string]any{"scope": "sbom"}
		if ref != "" {
			body["ref"] = ref
		}
		if sha != "" {
			body["ref"] = sha
		}
		response, err := api.GenerateSBOM(cmd.Context(), base+"/generate", body)
		if err != nil {
			return apiError(cfg, err, "generate SBOM")
		}
		taskID := fieldText(response.Fields, "ai_task_id")
		if taskID == "" {
			return errors.New("SBOM generation response did not include an analysis task ID")
		}
		result := map[string]any{"ai_task_id": taskID, "status": response.Fields["status"], "task_status": nil, "error_code": nil, "commit_sha": nil, "recorded_at": nil}
		expanded := false
		if value, ok := response.Fields["commit_sha"].(string); sha == "" && ok && fullSBOMCommit(value) {
			sha = value
			expanded = true
		}
		emit := func() (bool, error) {
			if !f.Changed("json") {
				return false, nil
			}
			fields, e := output.Fields(o.fields, sbomGenerateFields)
			if e == nil {
				e = output.JSON(cmd.Context(), d.Out, result, fields, o.expression)
			}
			if e != nil && strings.HasPrefix(e.Error(), "jq error:") {
				return true, &ExitError{1, "Error: " + e.Error()}
			}
			return true, e
		}
		if !wait {
			if sha != "" {
				result["commit_sha"] = sha
			}
			if done, e := emit(); done {
				return e
			}
			target := ""
			if sha != "" {
				target = " for " + sha[:12]
			}
			message := "SBOM generation started" + target + "."
			if result["status"] == "already_running" {
				message = "SBOM generation already running" + target + "; joined it."
			}
			if _, err = fmt.Fprintln(d.Out, message); err != nil {
				return err
			}
			_, err = fmt.Fprintln(d.Err, "Generation takes a few minutes; run \x60avr sbom view\x60 afterwards to see the result, or pass --wait to block until it finishes.")
			return err
		}
		_, _ = fmt.Fprintln(d.Err, "Waiting for SBOM generation…")
		poller := sbomPoller{d: d, api: api, cfg: cfg, deadline: deadline, taskID: taskID}
		tracker := sbomTaskTracker{poller: &poller, path: "/orgs/" + url.PathEscape(id) + "/repos/" + url.PathEscape(rid) + "/ai-task/state", id: taskID}
		waiter := sbomWaiter{poller: &poller, tracker: &tracker, timeout: timeout, result: result, emit: emit}
		err = func() error {
			if sha == "" {
				return waiter.taskOutcome(cmd.Context())
			}
			path := base + "/commits/" + sha
			if expanded {
				for {
					baseline, err = poller.get(cmd.Context(), path, "check for the commit's SBOM", true)
					var transient *sbomTransient
					if !errors.As(err, &transient) {
						break
					}
					more, e := poller.sleep(cmd.Context(), transient.backoff)
					if e != nil {
						return e
					}
					if !more {
						return waiter.timeOut("the SBOM for " + sha[:12] + " could not be checked")
					}
				}
				if err != nil {
					return err
				}
			}
			view := []string{"avr", "sbom", "view", sha, "--org", id, "--repo", rid}
			for i := range view {
				view[i] = shellQuotePython(view[i])
			}
			return waiter.commitDelivery(cmd.Context(), path, sha, baseline["recorded_at"], strings.Join(view, " "))
		}()
		var exit *ExitError
		if err != nil && !errors.As(err, &exit) {
			poller.stopped()
		}
		return err
	})
	return c
}
