package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

func (c *Client) CreateOrganization(ctx context.Context, name string) (map[string]any, error) {
	body, err := c.Do(ctx, http.MethodPost, "/users/me/organizations", map[string]any{"name": name})
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}
func (c *Client) OrganizationMembers(ctx context.Context, org string) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/members", nil)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) EmailDomains(ctx context.Context, path string) (Page, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) MutateEmailDomain(ctx context.Context, path string, body any) (map[string]any, error) {
	result, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	fields, ok := result["data"].(map[string]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected email domain data")
	}
	return decodeRecord(fields)
}
func (c *Client) SetEmailDomains(ctx context.Context, path string, domains []string) (Page, error) {
	body, err := c.Do(ctx, http.MethodPost, path, map[string]any{"domains": domains})
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

func (c *Client) Installations(ctx context.Context) (Page, error) {
	body, err := c.Get(ctx, "/users/me/installations", nil)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
