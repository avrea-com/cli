package client

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// decodeRecord checks fields the CLI uses to select resources, make decisions,
// or format output. Unknown fields and opaque payloads remain untouched.
// This is deliberately not a second implementation of the entire API schema.
func decodeRecord(fields map[string]any) (map[string]any, error) {
	if err := validateRecord(fields); err != nil {
		return nil, fmt.Errorf("unexpected API response: invalid record fields: %w", err)
	}
	return fields, nil
}

func validateRecord(fields map[string]any) error {
	// Sort for deterministic errors when several fields are malformed.
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := fields[key]
		if value == nil {
			continue
		} // Preserve missing versus explicit null.
		expected := ""
		switch key {
		case "organization_id", "repository_id", "run_id", "job_id", "customer_vm_id",
			"ai_task_id", "dispatch_id", "rule_id", "slug", "full_name", "name",
			"status", "state", "conclusion", "role", "platform", "ci_provider",
			"host", "external_ip", "username", "host_key", "protocol", "password", "token", "stripe_key", "client_secret":
			if _, ok := value.(string); !ok {
				expected = "string"
			}
		case "created_at", "updated_at", "started_at", "completed_at", "expires_at", "stop_at":
			text, ok := value.(string)
			if !ok {
				expected = "RFC3339 timestamp"
			} else if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				expected = "RFC3339 timestamp"
			}
		case "run_attempt", "run_number", "count", "port", "external_port", "platform_run_id", "platform_repository_id":
			if !recordInteger(value) {
				expected = "integer"
			}
		case "platform_job_id":
			// Some deployments send decimal IDs as strings. Preserve their
			// spelling and never convert them through floating point.
			if text, ok := value.(string); ok {
				if _, err := strconv.ParseInt(text, 10, 64); err != nil {
					expected = "decimal integer ID"
				}
			} else if !recordInteger(value) {
				expected = "integer or decimal string"
			}
		case "duration_seconds":
			if !recordNumber(value) {
				expected = "number"
			}
		case "enabled", "is_enforced", "ssh_keys_installed", "dispatchable", "can_dispatch",
			"has_more", "is_default", "enable_remote_desktop", "running_on_avrea",
			"allow_vm_create", "is_release", "has_billing", "has_pdf", "verified",
			"merged", "draft", "config_synced", "memory_included", "inherits",
			"over_quota", "egress_rules_applied", "https_ports_applied", "browsable":
			if _, ok := value.(bool); !ok {
				expected = "boolean"
			}
		case "endpoints":
			// VM endpoints are named records; mirror sessions use URL lists.
			switch endpoints := value.(type) {
			case map[string]any:
				for _, name := range []string{"ssh", "remote_desktop"} {
					if endpoints[name] == nil {
						continue
					}
					endpoint, ok := endpoints[name].(map[string]any)
					if !ok {
						return fmt.Errorf("endpoints.%s: expected object", name)
					}
					if err := validateRecord(endpoint); err != nil {
						return fmt.Errorf("endpoints.%s: %w", name, err)
					}
				}
			case []any:
				for i, endpoint := range endpoints {
					if _, ok := endpoint.(string); !ok {
						return fmt.Errorf("endpoints[%d]: expected string", i)
					}
				}
			default:
				expected = "object or URL array"
			}

		case "repository", "vm":
			child, ok := value.(map[string]any)
			if !ok {
				expected = "object"
			} else if err := validateRecord(child); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		case "jobs", "steps", "results", "rules", "placements", "egress_rules":
			rows, ok := value.([]any)
			if !ok {
				expected = "array"
			} else {
				for i, row := range rows {
					child, ok := row.(map[string]any)
					if !ok {
						return fmt.Errorf("%s[%d]: expected object", key, i)
					}
					if err := validateRecord(child); err != nil {
						return fmt.Errorf("%s[%d]: %w", key, i, err)
					}
				}
			}
		}
		if expected != "" {
			return fmt.Errorf("%s: expected %s", key, expected)
		}
	}
	return nil
}

func recordInteger(value any) bool {
	switch v := value.(type) {
	case json.Number:
		_, err := strconv.ParseInt(string(v), 10, 64)
		return err == nil
	case int, int64:
		return true
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Trunc(v) == v && v >= -9223372036854775808.0 && v < 9223372036854775808.0
	}
	return false
}

func recordNumber(value any) bool {
	switch v := value.(type) {
	case json.Number:
		// Validate spelling without converting or rounding the original.
		return json.Valid([]byte(v))
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case int, int64:
		return true
	}
	return false
}
