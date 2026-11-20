package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// SubmitAvreaCIRerun carries its durable key in the request body, as the API requires.
func (c *Client) SubmitAvreaCIRerun(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	content, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.submitAvreaCIRequest(ctx, path, content)
}

// DispatchAvreaCIWorkflow outlasts the 45 seconds the API may hold an Avrea CI
// dispatch. A request that repeats key returns the first one's receipt.
func (c *Client) DispatchAvreaCIWorkflow(ctx context.Context, org, repo, workflow, ref, key string, inputs map[string]string) (map[string]any, error) {
	request := map[string]any{"workflow": workflow, "ref": ref, "invocation_key": key}
	if len(inputs) > 0 {
		request["inputs"] = inputs
	}
	content, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	body, err := c.submitAvreaCIRequest(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/dispatch-workflow", content)
	if err != nil {
		return nil, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord(fields)
}

// WorkflowAdmission reads the receipt of an Avrea CI dispatch.
func (c *Client) WorkflowAdmission(ctx context.Context, org, repo, admission string) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/workflow-admissions/"+url.PathEscape(admission), nil)
	if err != nil {
		return nil, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord(fields)
}

func (c *Client) submitAvreaCIRequest(ctx context.Context, path string, content []byte) (map[string]any, error) {
	transport := *c.HTTP
	transport.Timeout = 60 * time.Second
	scoped := *c
	scoped.HTTP = &transport
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+c.Config.Token)
	data, err := scoped.request(ctx, http.MethodPost, path, rawRequestContent{data: content, contentType: "application/json", noTransportRetry: true}, headers, true)
	if err != nil {
		return nil, err
	}
	return decodeObject(data)
}
