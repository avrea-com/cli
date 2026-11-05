package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) SubmitFeedback(ctx context.Context, body wire.FeedbackRequest, key string) (Record[wire.FeedbackResponse], error) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+c.Config.Token)
	headers.Set("Idempotency-Key", key)
	raw, err := c.request(ctx, http.MethodPost, "/feedback", body, headers, true)
	if err != nil {
		return Record[wire.FeedbackResponse]{}, err
	}
	fields, err := decodeObject(raw)
	if err != nil {
		return Record[wire.FeedbackResponse]{}, err
	}
	return decodeFeedback(fields)
}

func (c *Client) Feedback(ctx context.Context, id string) (Record[wire.FeedbackResponse], error) {
	fields, err := c.Get(ctx, "/feedback/"+url.PathEscape(id), nil)
	if err != nil {
		return Record[wire.FeedbackResponse]{}, err
	}
	return decodeFeedback(fields)
}

func decodeFeedback(fields map[string]any) (Record[wire.FeedbackResponse], error) {
	record, err := decodeRecord[wire.FeedbackResponse](fields)
	if err != nil {
		return record, err
	}
	if record.Value.Id == "" || record.Value.Status == "" || record.Value.CreatedAt.IsZero() {
		return record, errors.New("invalid feedback response: missing id, status or created_at")
	}
	return record, nil
}
