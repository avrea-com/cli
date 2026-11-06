package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type AuditPage struct {
	Page
	EventData []json.RawMessage
}

func (c *Client) AuditEvents(ctx context.Context, org string, query url.Values) (AuditPage, error) {
	raw, err := c.Raw(ctx, http.MethodGet, "/orgs/"+url.PathEscape(org)+"/audit-events?"+query.Encode(), nil)
	if err != nil {
		return AuditPage{}, err
	}
	fields, err := decodeObject(raw)
	if err != nil {
		return AuditPage{}, err
	}
	page, err := decodePage(fields)
	if err != nil {
		return AuditPage{}, err
	}
	var envelope struct {
		Data []struct {
			EventData json.RawMessage `json:"event_data"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return AuditPage{}, err
	}
	result := AuditPage{Page: page}
	for _, row := range envelope.Data {
		result.EventData = append(result.EventData, row.EventData)
	}
	return result, nil
}
func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	fields, err := c.Get(ctx, "/health", nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(fields)
}
