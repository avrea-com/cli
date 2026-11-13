package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// ReviewAnchor is an immutable diff location: inclusive 1-based lines on one
// side of the comparison between BaseSHA and CommitSHA.
type ReviewAnchor struct {
	CommitSHA string `json:"commit_sha"`
	BaseSHA   string `json:"base_sha"`
	Path      string `json:"path"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func mirrorBrowsePath(org, repo string) string {
	return "/orgs/" + url.PathEscape(org) + "/repos/" + url.PathEscape(repo) + "/mirror"
}

// MirrorPullRequest reads one pull request's metadata, including the head and
// base commits that inline comments anchor to.
func (c *Client) MirrorPullRequest(ctx context.Context, org, repo string, number int) (map[string]any, error) {
	body, err := c.Get(ctx, mirrorBrowsePath(org, repo)+"/pulls/"+strconv.Itoa(number), nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}

// CreatePullRequestComment posts a general comment on the whole pull request.
func (c *Client) CreatePullRequestComment(ctx context.Context, org, repo string, number int, body string) (map[string]any, error) {
	fields, err := c.Do(ctx, http.MethodPost, mirrorBrowsePath(org, repo)+"/pulls/"+strconv.Itoa(number)+"/comments", map[string]any{"body": body})
	if err != nil {
		return nil, err
	}
	if !nonEmptyString(fields["id"]) {
		return nil, errors.New("unexpected API response: comment id is missing")
	}
	return decodeRecord(fields)
}

// CreateReviewThread starts an inline review thread and returns it with its
// first comment, which carries the comment's own GitHub publication status.
func (c *Client) CreateReviewThread(ctx context.Context, org, repo string, number int, body string, anchor ReviewAnchor) (thread, comment map[string]any, err error) {
	request := map[string]any{"pull_number": number, "body": body, "anchor": anchor}
	thread, err = c.Do(ctx, http.MethodPost, mirrorBrowsePath(org, repo)+"/review-threads", request)
	if err != nil {
		return nil, nil, err
	}
	if comments, _ := thread["comments"].([]any); len(comments) > 0 {
		comment, _ = comments[0].(map[string]any)
	}
	if !nonEmptyString(thread["id"]) || comment == nil || !nonEmptyString(comment["id"]) {
		return nil, nil, errors.New("unexpected API response: review thread or comment id is missing")
	}
	if comment, err = decodeRecord(comment); err != nil {
		return nil, nil, err
	}
	if thread, err = decodeRecord(thread); err != nil {
		return nil, nil, err
	}
	return thread, comment, nil
}

func nonEmptyString(value any) bool {
	text, ok := value.(string)
	return ok && text != ""
}
