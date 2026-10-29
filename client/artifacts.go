package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

// ArtifactRecord retains the server's field order for human-readable metadata.
type ArtifactRecord struct {
	Fields  map[string]any
	RawJSON json.RawMessage
}

func (c *Client) Artifacts(ctx context.Context, path string, query url.Values) (Page[wire.WorkflowArtifactView], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page[wire.WorkflowArtifactView]{}, err
	}
	return decodePage[wire.WorkflowArtifactView](body)
}

func (c *Client) Artifact(ctx context.Context, path string) (ArtifactRecord, error) {
	data, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return ArtifactRecord{}, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || envelope["data"] == nil {
		return ArtifactRecord{}, errors.New("unexpected API response: expected artifact data")
	}
	fields, err := decodeObject(envelope["data"])
	if err != nil {
		return ArtifactRecord{}, err
	}
	return ArtifactRecord{Fields: fields, RawJSON: envelope["data"]}, nil
}
