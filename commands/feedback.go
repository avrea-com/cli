package commands

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var feedbackFields = []string{"id", "status", "created_at"}
var feedbackSubmitFields = []string{"id", "status", "created_at", "idempotency_key"}
var feedbackIDPattern = lazyRegexp(`^fdb-[0-9a-f]{32}$`)
var feedbackOrgPattern = lazyRegexp(`^org-[0-9a-f]{32}$`)

// NewFeedback submits authenticated feedback and reads its delivery status.
func NewFeedback(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var bug bool
	var org, key string
	var o jsonOptions
	c := &cobra.Command{
		SuggestionsMinimumDistance: 2,
		Use:                        "feedback [MESSAGE]", Short: "Send feedback or report a bug.", Args: cobra.MaximumNArgs(1),
		Long: "Send MESSAGE to the Avrea team. Sign in with avr auth login first.\n\n" +
			"Use --bug for a bug report and --org SLUG_OR_ID to attribute it to an\n" +
			"organization. Without --org, feedback is personal, regardless of your default\n" +
			"organization. Contact details come from your account.\n\n" +
			"The idempotency key is printed before sending. If the response is lost or\n" +
			"the server asks you to retry, repeat the same message, --bug setting and organization\n" +
			"with --idempotency-key KEY. For a key conflict, use the original content\n" +
			"or omit --idempotency-key to create a new submission.\n" +
			"Acceptance queues delivery; use avr feedback status FEEDBACK_ID to check it.\n" +
			"Use -- before a message beginning with a dash, help, or a word resembling status.\n\n" +
			"Examples:\n" +
			"    avr feedback \"Please support Lambda Cloud\"\n" +
			"    avr feedback --bug \"Run logs are missing\"\n" +
			"    avr feedback status FEEDBACK_ID",
	}
	c.Flags().BoolVar(&bug, "bug", false, "Mark the feedback as a bug report.")
	c.Flags().StringVar(&org, "org", "", "Attribute feedback to this organization slug or ID.")
	invocationKeyFlag(c, &key, "Reuse a previous submission UUID (normalized to 32 lowercase hex characters).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, feedbackSubmitFields); done || err != nil {
			return err
		}
		if len(args) != 1 {
			return usageError(cmd, "Provide a quoted feedback MESSAGE.")
		}
		message := strings.TrimSpace(args[0])
		if !utf8.ValidString(message) || utf8.RuneCountInString(message) < 1 || utf8.RuneCountInString(message) > 10000 {
			return usageError(cmd, "MESSAGE must contain 1 to 10000 Unicode characters after trimming whitespace.")
		}
		if cmd.ArgsLenAtDash() != 0 && len(strings.Fields(message)) == 1 && (strings.EqualFold(message, "help") || len(cmd.SuggestionsFor(message)) > 0) {
			return usageError(cmd, "Use avr feedback --help for help or avr feedback status FEEDBACK_ID for status. To send this literal message, use avr feedback -- MESSAGE.")
		}
		if cmd.Flags().Changed("org") && (strings.TrimSpace(org) == "" || strings.HasPrefix(org, "org-") && !feedbackOrgPattern().MatchString(org)) {
			return usageError(cmd, "--org must be an organization slug or canonical ID.")
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = feedbackAuthenticated(cfg); err != nil {
			return err
		}
		kind := "feedback"
		if bug {
			kind = "bug"
		}
		body := map[string]any{"message": message, "kind": kind}
		if cmd.Flags().Changed("org") {
			id, e := resolveOrg(cmd.Context(), d, cfg, api, org)
			if e != nil {
				return e
			}
			body["organization_id"] = id
		}
		if key == "" {
			id, e := uuid.NewRandom()
			if e != nil {
				return e
			}
			key = strings.ReplaceAll(id.String(), "-", "")
		}
		// Emit before the mutation so a lost response still leaves a safe retry key.
		if _, err = fmt.Fprintln(d.Err, "Idempotency key: "+key); err != nil {
			return err
		}
		record, err := api.SubmitFeedback(cmd.Context(), body, key)
		if err != nil {
			return feedbackSubmissionError(cfg, err, key)
		}
		record["idempotency_key"] = key
		if err := renderFeedback(cmd, d, o, record, feedbackSubmitFields); err != nil {
			return err
		}
		if !cmd.Flags().Changed("json") {
			_, err = fmt.Fprintf(d.Out, "\nCheck delivery: avr feedback status %s\n", output.CellText(fieldText(record, "id")))
		}
		return err
	})
	c.AddCommand(newFeedbackStatus(d))
	return c
}

func feedbackSubmissionError(cfg config.Config, err error, key string) error {
	result := apiError(cfg, err, "submit feedback")
	hint := "Retry the same message, --bug setting and organization with --idempotency-key " + key + "."
	var response *client.HTTPError
	if errors.As(err, &response) {
		switch {
		case response.Status == 409:
			hint = "This key belongs to different content. Resend the original content, or omit --idempotency-key to create a new submission."
		case response.Status == 429 || response.Status >= 500 && response.Status < 600:
		default:
			return result
		}
	}
	return fmt.Errorf("%w\n  Hint: %s", result, hint)
}

func newFeedbackStatus(d Dependencies) *cobra.Command {
	var o jsonOptions
	c := &cobra.Command{Use: "status FEEDBACK_ID", Short: "Read the delivery status of your feedback.", Args: positionalArguments("FEEDBACK_ID")}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, feedbackFields); done || err != nil {
			return err
		}
		if !feedbackIDPattern().MatchString(args[0]) {
			return usageError(cmd, "FEEDBACK_ID must be fdb- followed by 32 lowercase hex characters.")
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = feedbackAuthenticated(cfg); err != nil {
			return err
		}
		record, err := api.Feedback(cmd.Context(), args[0])
		if err != nil {
			return apiError(cfg, err, "read feedback status")
		}
		return renderFeedback(cmd, d, o, record, feedbackFields)
	})
	return c
}

func renderFeedback(cmd *cobra.Command, d Dependencies, o jsonOptions, fields map[string]any, available []string) error {
	if cmd.Flags().Changed("json") {
		return o.record(cmd, d, fields, available)
	}
	return output.KeyValues(d.Out, [][2]string{
		{"Feedback:", fieldText(fields, "id")},
		{"Status:", fieldText(fields, "status")},
		{"Created:", fieldText(fields, "created_at")},
	})
}

func feedbackAuthenticated(cfg config.Config) error {
	if err := authenticated(cfg); err != nil {
		return err
	}
	if config.IsScoped(cfg.Token) {
		return &ExitError{4, "Error: Feedback needs a signed-in account; scoped tokens cannot send or read feedback.\n  Hint: unset AVR_TOKEN if it contains the scoped token, then run avr auth login."}
	}
	return nil
}
