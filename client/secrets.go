package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Availability contains the selector categories understood by the secret manager.
// Empty categories are unrestricted; values within each category are alternatives.
type Availability struct {
	Repositories []string `json:"repositories,omitempty"`
	Environments []string `json:"environments,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Workflows    []string `json:"workflows,omitempty"`
	PrivateOnly  bool     `json:"private_only,omitempty"`
}

type ScopedValue struct {
	VariantID               string       `json:"variant_id,omitempty"`
	VariableVariantID       string       `json:"variable_variant_id,omitempty"`
	Label                   *string      `json:"label"`
	Availability            Availability `json:"availability"`
	RotationReminderDays    *int         `json:"rotation_reminder_days,omitempty"`
	Value                   *string      `json:"value,omitempty"`
	HasValue                bool         `json:"has_value"`
	CurrentVersionID        *string      `json:"current_version_id"`
	CurrentVersion          *int         `json:"current_version"`
	CurrentVersionCreatedAt *string      `json:"current_version_created_at"`
	UpdatedAt               *string      `json:"updated_at"`
}

func (v ScopedValue) ID() string {
	if v.VariantID != "" {
		return v.VariantID
	}
	return v.VariableVariantID
}

type ConfigurationValue struct {
	SecretID    string        `json:"secret_id,omitempty"`
	VariableID  string        `json:"variable_id,omitempty"`
	Name        string        `json:"name"`
	Description *string       `json:"description"`
	AsFile      bool          `json:"as_file"`
	UpdatedAt   *string       `json:"updated_at"`
	Variants    []ScopedValue `json:"variants"`
}

func (v ConfigurationValue) ID() string {
	if v.SecretID != "" {
		return v.SecretID
	}
	return v.VariableID
}

type ValueWrite struct {
	VersionID string `json:"version_id"`
	Status    string `json:"status"`
}

// ConfigurationRequest deliberately disables diagnostics and transport replay.
// A value PUT appends a version, so retrying it is not an idempotent replacement.
// Error details are hidden by default because proxies can echo plaintext.
// Raw payloads and diagnostics are never exposed.
func (c *Client) ConfigurationRequest(ctx context.Context, method, path string, body any, target any) error {
	return c.configurationRequest(ctx, method, path, body, target, false)
}

// ConfigurationMetadataRequest opts into parsed refusal details. Callers must
// use it only for requests that do not carry secret or variable values.
func (c *Client) ConfigurationMetadataRequest(ctx context.Context, method, path string, body any, target any) error {
	return c.configurationRequest(ctx, method, path, body, target, true)
}

func (c *Client) configurationRequest(ctx context.Context, method, path string, body any, target any, allowDetails bool) error {
	headers := http.Header{"Authorization": {"Bearer " + c.Config.Token}}
	var payload any
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return errors.New("could not encode configuration request")
		}
		payload = rawRequestContent{data: data, contentType: "application/json", noTransportRetry: true}
	}
	data, err := c.request(ctx, method, path, payload, headers, false)
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) {
			safe := &HTTPError{Path: path, Status: he.Status, Reauthentication: he.Reauthentication, RetryAfter: he.RetryAfter}
			if allowDetails && (he.Status >= 400 && he.Status < 500 || he.Status == 503) {
				safe.Detail = he.Detail
			}
			return safe
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if method == http.MethodGet {
			return err
		}
		return errors.New("configuration request could not be confirmed")
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("invalid configuration response")
	}
	return nil
}
