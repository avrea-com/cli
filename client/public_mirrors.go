package client

import (
	"bytes"
	"context"
	"net/http"
)

func (c *Client) PublicMirror(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}
func (c *Client) PublicMirrorRequests(ctx context.Context, path string) (Page, error) {
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page{}, err
	}
	return decodeArray(raw)
}
func (c *Client) PublicMirrorRequest(ctx context.Context, method, path string, body any, fallbackID string) (map[string]any, error) {
	raw, err := c.Raw(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if fallbackID != "" && (len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))) {
		fields = map[string]any{"request_id": fallbackID}
	} else {
		fields, err = decodeObject(raw)
		if err != nil {
			return nil, err
		}
		if len(fields) == 0 && fallbackID != "" {
			fields["request_id"] = fallbackID
		}
	}
	return decodeRecord(fields)
}
