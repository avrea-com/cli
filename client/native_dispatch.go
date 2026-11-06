package client

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// SubmitNativeDispatch keeps decimal input bytes and the invocation key identical
// across retries. Its longer source-check budget is local to this request.
func (c *Client) SubmitNativeDispatch(ctx context.Context, path string, content []byte, key string) (map[string]any, error) {
	return c.submitNativeRequest(ctx, path, content, key)
}

// SubmitNativeRerun carries its durable key in the request body, as the API requires.
func (c *Client) SubmitNativeRerun(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	content, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.submitNativeRequest(ctx, path, content, "")
}

func (c *Client) submitNativeRequest(ctx context.Context, path string, content []byte, key string) (map[string]any, error) {
	transport := *c.HTTP
	transport.Timeout = 60 * time.Second
	scoped := *c
	scoped.HTTP = &transport
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+c.Config.Token)
	if key != "" {
		headers.Set("Idempotency-Key", key)
	}
	data, err := scoped.request(ctx, http.MethodPost, path, rawRequestContent{data: content, contentType: "application/json", noTransportRetry: true}, headers, true)
	if err != nil {
		return nil, err
	}
	return decodeObject(data)
}
