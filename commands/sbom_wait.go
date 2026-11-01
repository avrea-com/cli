package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
)

type sbomTransient struct{ backoff time.Duration }

func (e *sbomTransient) Error() string { return "transient SBOM poll failure" }

type sbomPoller struct {
	d                 Dependencies
	api               *client.Client
	cfg               config.Config
	deadline          time.Time
	taskID, lastError string
	stoppedPrinted    bool
}

func (p *sbomPoller) stopped() {
	if p.stoppedPrinted {
		return
	}
	p.stoppedPrinted = true
	_, _ = fmt.Fprintf(p.d.Err, "Stopped waiting; SBOM generation was started as task %s. Check \x60avr sbom list\x60 later.\n", output.CellText(p.taskID))
}

var sbomRetrySeconds = lazyRegexp("^[0-9]+$")

func sbomRetryBackoff(value string) time.Duration {
	value = strings.TrimSpace(value)
	if !sbomRetrySeconds().MatchString(value) {
		return 5 * time.Second
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil || seconds > 30 {
		return 30 * time.Second
	}
	return time.Duration(seconds) * time.Second
}
func (p *sbomPoller) get(ctx context.Context, path, action string, missingOK bool) (map[string]any, error) {
	timeout := max(p.deadline.Sub(p.d.Now()), time.Second)
	if p.api.HTTP.Timeout > 0 {
		timeout = min(timeout, p.api.HTTP.Timeout)
	}
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var fields map[string]any
	var err error
	if missingOK {
		record, e := p.api.SBOM(pollCtx, path)
		fields, err = record.Fields, e
	} else {
		record, e := p.api.SBOMTaskState(pollCtx, path)
		fields, err = record.Fields, e
	}
	if err == nil {
		p.lastError = ""
		return fields, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var he *client.HTTPError
	var transport *client.TransportError
	var description string
	backoff := 5 * time.Second
	switch {
	case errors.As(err, &he):
		if missingOK && he.Status == 404 {
			p.lastError = ""
			return nil, nil
		}
		if he.Status < 500 && he.Status != 408 && he.Status != 429 {
			p.stopped()
			return nil, apiError(p.cfg, err, action)
		}
		description = fmt.Sprintf("HTTP %d", he.Status)
		if he.Status == 429 {
			backoff = sbomRetryBackoff(he.RetryAfter)
		}
	case errors.Is(pollCtx.Err(), context.DeadlineExceeded):
		description = "ReadTimeout"
	case errors.As(err, &transport):
		description = transport.Reason
	default:
		return nil, err
	}
	p.lastError = description
	_, _ = fmt.Fprintf(p.d.Err, "Warning: could not %s (%s); retrying.\n", action, description)
	return nil, &sbomTransient{backoff}
}
func (p *sbomPoller) sleep(ctx context.Context, backoff time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	remaining := p.deadline.Sub(p.d.Now())
	if remaining <= 0 {
		return false, nil
	}
	return true, p.d.Wait(ctx, min(backoff, remaining))
}

type sbomTaskTracker struct {
	poller                       *sbomPoller
	path, id, status, superseded string
	seen                         bool
	errorCode                    any
}

func (t *sbomTaskTracker) settled() bool {
	return t.status == "COMPLETED" || t.status == "FAILED" || t.superseded != ""
}
func (t *sbomTaskTracker) taskStatus() string {
	if t.superseded != "" || !t.seen {
		return "unknown"
	}
	return strings.ToLower(t.status)
}
func (t *sbomTaskTracker) poll(ctx context.Context) error {
	if t.settled() {
		return nil
	}
	state, err := t.poller.get(ctx, t.path, "check SBOM generation status", false)
	if err != nil {
		return err
	}
	observed := fieldText(state, "ai_task_id")
	if observed == t.id {
		t.status = fieldText(state, "status")
		t.seen = state["status"] != nil
		t.errorCode = state["error_code"]
	} else if t.seen && observed != "" {
		t.superseded = observed
	}
	return nil
}
func (t *sbomTaskTracker) failureCode() string {
	if s, ok := t.errorCode.(string); ok && s != "" {
		return output.CellText(s)
	}
	return "unknown error"
}

type sbomWaiter struct {
	poller  *sbomPoller
	tracker *sbomTaskTracker
	timeout int
	result  map[string]any
	emit    func() (bool, error)
}

func (w *sbomWaiter) record(status string) {
	w.result["status"] = status
	w.result["task_status"] = w.tracker.taskStatus()
	w.result["error_code"] = w.tracker.errorCode
}
func (w *sbomWaiter) timeOut(pending string) error {
	w.record("timeout")
	done, err := w.emit()
	if err != nil {
		return err
	}
	if !done {
		last := ""
		if w.poller.lastError != "" {
			last = " (last error: " + w.poller.lastError + ")"
		}
		_, _ = fmt.Fprintf(w.poller.d.Err, "Error: Timed out after %ds waiting for task %s%s; %s. The analysis was not cancelled and may still finish; check \x60avr sbom list\x60 later.\n", w.timeout, output.CellText(w.tracker.id), last, pending)
	}
	return &ExitError{1, ""}
}
func (w *sbomWaiter) taskOutcome(ctx context.Context) error {
	t := w.tracker
	for {
		backoff := 5 * time.Second
		err := t.poll(ctx)
		var transient *sbomTransient
		if errors.As(err, &transient) {
			backoff = transient.backoff
		} else if err != nil {
			return err
		}
		if t.settled() {
			break
		}
		more, err := w.poller.sleep(ctx, backoff)
		if err != nil {
			return err
		}
		if !more {
			return w.timeOut("its outcome is not known yet")
		}
	}
	w.record(t.taskStatus())
	done, err := w.emit()
	if err != nil {
		return err
	}
	if !done {
		switch {
		case t.superseded != "":
			_, err = fmt.Fprintf(w.poller.d.Err, "Error: Analysis task %s was superseded by task %s before its outcome could be read; check \x60avr sbom list\x60.\n", output.CellText(t.id), output.CellText(t.superseded))
		case t.status == "FAILED":
			_, err = fmt.Fprintf(w.poller.d.Err, "Error: Analysis task %s failed (%s). A joined full analysis can still record its SBOM; check \x60avr sbom list\x60.\n", output.CellText(t.id), t.failureCode())
		default:
			_, err = fmt.Fprintln(w.poller.d.Out, "Analysis completed. Its SBOM can take a moment to appear in \x60avr sbom list\x60; pass --ref <commit-sha> with --wait to wait for a specific commit's SBOM.")
		}
	}
	if err != nil {
		return err
	}
	if t.taskStatus() != "completed" {
		return &ExitError{1, ""}
	}
	return nil
}
func (w *sbomWaiter) commitDelivery(ctx context.Context, path, sha string, baseline any, view string) error {
	t := w.tracker
	for {
		snapshot, err := w.poller.get(ctx, path, "check for the commit's SBOM", true)
		if err == nil && snapshot != nil && snapshot["recorded_at"] != baseline {
			w.record("completed")
			w.result["commit_sha"] = snapshot["commit_sha"]
			w.result["recorded_at"] = snapshot["recorded_at"]
			if done, e := w.emit(); done {
				return e
			}
			if t.status == "FAILED" {
				_, _ = fmt.Fprintf(w.poller.d.Err, "Note: analysis task %s failed (%s) after producing the SBOM.\n", output.CellText(t.id), t.failureCode())
			}
			_, err = fmt.Fprintf(w.poller.d.Out, "SBOM generated for %s. Run \x60%s\x60 to see it.\n", sha[:12], output.CellText(view))
			return err
		}
		if err == nil {
			err = t.poll(ctx)
		}
		backoff := 5 * time.Second
		var transient *sbomTransient
		if errors.As(err, &transient) {
			backoff = transient.backoff
		} else if err != nil {
			return err
		}
		more, err := w.poller.sleep(ctx, backoff)
		if err != nil {
			return err
		}
		if more {
			continue
		}
		if !t.settled() {
			return w.timeOut("no new SBOM for " + sha[:12] + " yet")
		}
		w.record("failed")
		done, err := w.emit()
		if err != nil {
			return err
		}
		if !done {
			var message string
			switch {
			case t.superseded != "":
				message = fmt.Sprintf("was superseded by task %s and no new SBOM for %s was delivered within %ds.", output.CellText(t.superseded), sha[:12], w.timeout)
			case t.status == "FAILED":
				message = fmt.Sprintf("failed (%s) and no new SBOM for %s was delivered within %ds.", t.failureCode(), sha[:12], w.timeout)
			default:
				message = fmt.Sprintf("completed, but no new SBOM for %s was available within %ds. Artifact delivery may still be retrying; check \x60avr sbom list\x60 later.", sha[:12], w.timeout)
			}
			_, _ = fmt.Fprintf(w.poller.d.Err, "Error: Analysis task %s %s\n", output.CellText(t.id), message)
		}
		return &ExitError{1, ""}
	}
}
