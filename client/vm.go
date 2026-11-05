package client

import (
	"context"
	"errors"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func vmData[T any](body map[string]any) (Record[T], error) {
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return Record[T]{}, errors.New("unexpected API response: expected VM data")
	}
	return decodeRecord[T](fields)
}
func (c *Client) VMs(ctx context.Context, path string, query url.Values) (Page[wire.CustomerVm], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page[wire.CustomerVm]{}, err
	}
	return decodePage[wire.CustomerVm](body)
}
func (c *Client) VM(ctx context.Context, path string) (Record[wire.CustomerVmDetail], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[wire.CustomerVmDetail]{}, err
	}
	return vmData[wire.CustomerVmDetail](body)
}
func (c *Client) VMUsage(ctx context.Context, path string, query url.Values) (Record[wire.CustomerVmUsageReport], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Record[wire.CustomerVmUsageReport]{}, err
	}
	return vmData[wire.CustomerVmUsageReport](body)
}

// VMCapabilities covers the capabilities endpoint added before its response was
// included in the pinned OpenAPI schema. Unknown fields remain in Record.Fields.
type VMCapabilities struct {
	MaxActiveVCPUs   *int             `json:"max_active_vcpus"`
	ActiveVCPUs      *int             `json:"active_vcpus"`
	MaxActivePerUser *int             `json:"max_active_per_user"`
	ActiveCount      *int             `json:"active_count"`
	OSTypes          []map[string]any `json:"os_types"`
}

func (c *Client) VMCapabilities(ctx context.Context, path string) (Record[VMCapabilities], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[VMCapabilities]{}, err
	}
	return vmData[VMCapabilities](body)
}

type VMMetrics struct {
	Metrics map[string]map[string]any `json:"metrics"`
}

func (c *Client) VMMetrics(ctx context.Context, path string, query url.Values) (Record[VMMetrics], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Record[VMMetrics]{}, err
	}
	return vmData[VMMetrics](body)
}
