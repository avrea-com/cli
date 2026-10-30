package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) CreateOrganization(ctx context.Context, name string) (Record[wire.OrganizationListItem], error) {
	body, err := c.Do(ctx, http.MethodPost, "/users/me/organizations", map[string]any{"name": name})
	if err != nil {
		return Record[wire.OrganizationListItem]{}, err
	}
	return decodeRecord[wire.OrganizationListItem](body)
}
func (c *Client) OrganizationMembers(ctx context.Context, org string) (Page[wire.OrganizationMemberResponse], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/members", nil)
	if err != nil {
		return Page[wire.OrganizationMemberResponse]{}, err
	}
	return decodePage[wire.OrganizationMemberResponse](body)
}
func (c *Client) EmailDomains(ctx context.Context, path string) (Page[wire.EmailDomainResponse], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Page[wire.EmailDomainResponse]{}, err
	}
	return decodePage[wire.EmailDomainResponse](body)
}
func (c *Client) MutateEmailDomain(ctx context.Context, path string, body any) (Record[wire.EmailDomainResponse], error) {
	result, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return Record[wire.EmailDomainResponse]{}, err
	}
	fields, ok := result["data"].(map[string]any)
	if !ok {
		return Record[wire.EmailDomainResponse]{}, errors.New("unexpected API response: expected email domain data")
	}
	return decodeRecord[wire.EmailDomainResponse](fields)
}
func (c *Client) SetEmailDomains(ctx context.Context, path string, domains []string) (Page[wire.EmailDomainResponse], error) {
	body, err := c.Do(ctx, http.MethodPost, path, map[string]any{"domains": domains})
	if err != nil {
		return Page[wire.EmailDomainResponse]{}, err
	}
	return decodePage[wire.EmailDomainResponse](body)
}
