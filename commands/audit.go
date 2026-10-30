package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var auditFields = []string{"event_id", "created_at", "actor_user_id", "actor_type", "acting_api_key_id", "client_ip", "resource_type", "resource_id", "action", "event_data"}

// NewAudit exposes organization write history.
func NewAudit(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "audit-events", Short: "View audit events for organization writes."}
	var org, resource, action, actor, after, before, cursor string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List audit events for the organization.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	f := c.Flags()
	f.StringVar(&resource, "resource-type", "", "Filter by resource type.")
	f.StringVar(&action, "action", "", "Filter by action.")
	f.StringVar(&actor, "actor-user-id", "", "Filter by acting user id.")
	f.StringVar(&after, "from", "", "ISO-8601 lower bound (inclusive) on created_at.")
	f.StringVar(&after, "created-after", "", "Alias for --from.")
	f.StringVar(&before, "to", "", "ISO-8601 upper bound (exclusive) on created_at.")
	f.StringVar(&before, "created-before", "", "Alias for --to.")
	f.StringVar(&cursor, "cursor", "", "Opaque cursor from a previous response.")
	f.IntVarP(&limit, "limit", "L", 100, "Max events to return (1–1000).")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := o.prepare(cmd, d, auditFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if f.Changed("cursor") && (cursor == "" || strings.TrimSpace(cursor) != cursor || utf8.RuneCountInString(cursor) > 512) {
			return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		for key, value := range map[string]string{"resource_type": resource, "action": action, "actor_user_id": actor, "created_after": after, "created_before": before, "cursor": cursor} {
			if value != "" {
				query.Set(key, value)
			}
		}
		page, err := api.AuditEvents(cmd.Context(), id, query)
		if err != nil {
			return apiError(cfg, err, "list audit events")
		}
		rows := rawRecords(page.Page)
		if f.Changed("json") {
			return o.list(cmd, d, rows, auditFields)
		}
		if len(rows) == 0 {
			if _, err = fmt.Fprintln(d.Out, "No data found."); err != nil {
				return err
			}
		} else {
			var cells [][]string
			for i, row := range rows {
				data := "{}"
				if raw := bytes.TrimSpace(page.EventData[i]); len(raw) > 0 && string(raw) != "null" {
					decoder := json.NewDecoder(bytes.NewReader(raw))
					decoder.UseNumber()
					data, err = auditJSONValue(decoder, 0)
					if err != nil {
						return err
					}
				}
				actorLabel := pythonValue(fallback(row["actor_user_id"], pythonValue(fallback(row["actor_type"], "-"))))
				cells = append(cells, []string{formatTimestamp(fieldText(row, "created_at")), actorLabel, organizationCell(row, "actor_type"), organizationCell(row, "resource_type"), pythonValue(fallback(row["resource_id"], "-")), organizationCell(row, "action"), data})
			}
			if err = output.Grid(d.Out, []string{"Time", "Actor", "Type", "Resource", "Target", "Action", "Data"}, cells); err != nil {
				return err
			}
		}
		if page.NextCursor != "" {
			_, err = fmt.Fprintf(d.Err, "\nMore results available. Re-run with --cursor %s\n", output.CellText(page.NextCursor))
		}
		return err
	})
	group.AddCommand(c)
	return group
}

// Keep API member order and Python's ASCII JSON strings in the table's Data
// cell. The independent JSON output path retains typed, unmodified values.
func auditJSONValue(decoder *json.Decoder, depth int) (string, error) {
	if depth > 100 {
		return "", fmt.Errorf("audit data exceeds nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	switch value := token.(type) {
	case json.Delim:
		var values []string
		for decoder.More() {
			prefix := ""
			if value == '{' {
				key, err := decoder.Token()
				if err != nil {
					return "", err
				}
				prefix = auditJSONString(key.(string)) + ":"
			}
			child, err := auditJSONValue(decoder, depth+1)
			if err != nil {
				return "", err
			}
			values = append(values, prefix+child)
		}
		if _, err = decoder.Token(); err != nil {
			return "", err
		}
		if value == '{' {
			return "{" + strings.Join(values, ",") + "}", nil
		}
		return "[" + strings.Join(values, ",") + "]", nil
	case string:
		return auditJSONString(value), nil
	case json.Number:
		return value.String(), nil
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(value), nil
	default:
		return "", fmt.Errorf("unexpected audit data")
	}
}
func auditJSONString(value string) string {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	var result strings.Builder
	for _, r := range strings.TrimSuffix(b.String(), "\n") {
		if r <= 127 {
			result.WriteRune(r)
		} else {
			for _, unit := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&result, "\\u%04x", unit)
			}
		}
	}
	return result.String()
}
