package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

func (c *Client) SubmitFeedback(ctx context.Context, body any, key string) (map[string]any, error) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+c.Config.Token)
	headers.Set("Idempotency-Key", key)
	raw, err := c.request(ctx, http.MethodPost, "/feedback", body, headers, true)
	if err != nil {
		return nil, err
	}
	fields, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	return decodeFeedback(fields)
}

func (c *Client) Feedback(ctx context.Context, id string) (map[string]any, error) {
	fields, err := c.Get(ctx, "/feedback/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	return decodeFeedback(fields)
}

func decodeFeedback(fields map[string]any) (map[string]any, error) {
	for _, key := range []string{"id", "status", "created_at"} {
		value, ok := fields[key].(string)
		if !ok || value == "" {
			return nil, errors.New("invalid feedback response: missing id, status or created_at")
		}
	}
	return decodeRecord(fields)
}
