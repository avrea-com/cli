package client

import (
	"context"
	"maps"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) SBOMs(ctx context.Context, path string, query url.Values) (Page[wire.SbomListItem], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page[wire.SbomListItem]{}, err
	}
	return decodePage[wire.SbomListItem](body)
}
func (c *Client) SBOM(ctx context.Context, path string) (Record[wire.AnalysisTaskResponse], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[wire.AnalysisTaskResponse]{}, err
	}
	// Older producers can deliver a non-object opaque summary. Keep the raw
	// JSON for projection while letting human output treat it as unavailable.
	typed := maps.Clone(body)
	if _, ok := body["summary"].(map[string]any); !ok {
		typed["summary"] = nil
	}
	record, err := decodeRecord[wire.AnalysisTaskResponse](typed)
	record.Fields = body
	return record, err
}
