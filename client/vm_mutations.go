package client

import (
	"bytes"
	"context"
	"net/http"

	"github.com/avrea-com/cli/client/wire"
)

// VMLegacyPowerRequest preserves Python's start/stop wire contract. desired_state
// is absent from the pinned current schema; do not silently map stop to pause
// or delete, which have different persistence and password semantics.
type VMLegacyPowerRequest struct {
	DesiredState string `json:"desired_state"`
}

func (c *Client) ChangeVM(ctx context.Context, path string, body any) (Record[wire.APIResponseCustomerVmWithPassword], error) {
	response, err := c.Do(ctx, http.MethodPatch, path, body)
	if err != nil {
		return Record[wire.APIResponseCustomerVmWithPassword]{}, err
	}
	return decodeRecord[wire.APIResponseCustomerVmWithPassword](response)
}
func (c *Client) TransitionVM(ctx context.Context, path string, body any) (Record[wire.APIResponseCustomerVm], error) {
	response, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return Record[wire.APIResponseCustomerVm]{}, err
	}
	return decodeRecord[wire.APIResponseCustomerVm](response)
}
func (c *Client) DeleteVM(ctx context.Context, path string) (Record[wire.APIResponseCustomerVmDeleteResponse], error) {
	raw, err := c.Raw(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return Record[wire.APIResponseCustomerVmDeleteResponse]{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return Record[wire.APIResponseCustomerVmDeleteResponse]{}, nil
	}
	body, err := decodeObject(raw)
	if err != nil {
		return Record[wire.APIResponseCustomerVmDeleteResponse]{}, err
	}
	return decodeRecord[wire.APIResponseCustomerVmDeleteResponse](body)
}
