package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var prCommentFields = []string{"id", "thread_id", "pull_number", "repository_id", "path", "side", "start_line", "end_line", "commit_sha", "base_sha", "body", "publication", "created_at"}

const prCommentsFeature = "feature.pr-comments.enabled"

// Limits from the public OpenAPI schema (PullRequestCommentCreate,
// ReviewThreadCreate, ReviewAnchor), counted in Unicode code points like the
// server's Pydantic validation. Numbers are PostgreSQL INTEGER columns.
const (
	prCommentBodyLimit       = 262144
	prInlineCommentBodyLimit = 65536
	prCommentPathLimit       = 4096
	prCommentMaxNumber       = 1<<31 - 1
)

var reviewCommitSHA = lazyRegexp(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)
var prNumberPattern = lazyRegexp(`^[0-9]+$`)

func newPRComment(d Dependencies) *cobra.Command {
	var o jsonOptions
	var org, repo, body, bodyFile, path, side string
	var line, startLine int
	c := &cobra.Command{Use: "comment NUMBER", Short: "Comment on a pull request.", Args: positionalArguments("NUMBER")}
	f := c.Flags()
	f.StringVar(&body, "body", "", "Comment text in Markdown.")
	f.StringVar(&bodyFile, "body-file", "", "Read the comment text from a file, or - for stdin.")
	f.StringVar(&path, "path", "", "Comment inline on this repository-relative file.")
	f.IntVar(&line, "line", 0, "Line to comment on, or the last line of a range.")
	f.IntVar(&startLine, "start-line", 0, "First line of a multi-line inline comment.")
	f.StringVar(&side, "side", "right", "Diff side: right (new file) or left (old file).")
	f.StringVar(&repo, "repo", "", "Repository name or ID; otherwise detect from Git.")
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, prCommentFields); done || err != nil {
			return err
		}
		number, err := strconv.Atoi(args[0])
		if !prNumberPattern().MatchString(args[0]) || err != nil || number < 1 || number > prCommentMaxNumber {
			return &ExitError{2, "Error: NUMBER must be a pull request number, such as 42."}
		}
		inline := f.Changed("path")
		var anchor client.ReviewAnchor
		switch {
		case inline && !f.Changed("line"):
			return &ExitError{2, "Error: --path requires --line."}
		case !inline && f.Changed("line"):
			return &ExitError{2, "Error: --line requires --path."}
		case !inline && (f.Changed("start-line") || f.Changed("side")):
			return &ExitError{2, "Error: --start-line and --side require --path and --line."}
		case inline:
			if anchor, err = prCommentAnchor(path, side, line, startLine, f.Changed("start-line")); err != nil {
				return err
			}
		}
		limit, kind := prCommentBodyLimit, "pull request comments"
		if inline {
			limit, kind = prInlineCommentBodyLimit, "inline comments"
		}
		text, err := prCommentBody(cmd, d, body, bodyFile, limit, kind)
		if err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		// The API closes every pull-request write to scoped tokens, and its
		// feature-flag read would otherwise answer them with a misleading 404.
		if config.IsScoped(cfg.Token) {
			return &ExitError{4, "Error: Commenting on pull requests needs a signed-in account; scoped tokens cannot post comments.\n  Hint: unset AVR_TOKEN if it contains the scoped token, then run avr auth login."}
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		goal := "comment on pull request #" + strconv.Itoa(number)
		if err = requirePRFeature(cmd.Context(), cfg, api, id, prCommentsFeature, "pull request commenting", goal); err != nil {
			return err
		}
		rid, label, err := requiredNamedRepository(cmd.Context(), d, cfg, api, id, repo)
		if err != nil {
			return err
		}
		var record map[string]any
		if inline {
			pull, e := api.MirrorPullRequest(cmd.Context(), id, rid, number)
			if e != nil {
				return prRequestError(cfg, e, goal, "read pull request #"+strconv.Itoa(number))
			}
			head, base := fieldText(pull, "head_sha"), fieldText(pull, "base_sha")
			if !reviewCommitSHA().MatchString(head) || !reviewCommitSHA().MatchString(base) {
				return &ExitError{1, fmt.Sprintf("Error: Avrea has not synced the head and base commits of pull request #%d yet. Try again shortly.", number)}
			}
			anchor.CommitSHA, anchor.BaseSHA = strings.ToLower(head), strings.ToLower(base)
			thread, comment, e := api.CreateReviewThread(cmd.Context(), id, rid, number, text, anchor)
			if e != nil {
				return prCommentError(cfg, e, goal)
			}
			record = prCommentRecord(comment, number, rid)
			record["thread_id"] = thread["id"]
			record["path"], record["side"], record["start_line"], record["end_line"] = anchor.Path, anchor.Side, anchor.StartLine, anchor.EndLine
			record["commit_sha"], record["base_sha"] = anchor.CommitSHA, anchor.BaseSHA
		} else {
			created, e := api.CreatePullRequestComment(cmd.Context(), id, rid, number, text)
			if e != nil {
				return prCommentError(cfg, e, goal)
			}
			record = prCommentRecord(created, number, rid)
		}
		if f.Changed("json") {
			return o.record(cmd, d, record, prCommentFields)
		}
		_, err = fmt.Fprintln(d.Out, prCommentSummary(label, number, record))
		return err
	})
	return c
}

func prCommentAnchor(path, side string, line, startLine int, rangeGiven bool) (client.ReviewAnchor, error) {
	anchor := client.ReviewAnchor{Path: path, StartLine: line, EndLine: line}
	if !validReviewPath(path) {
		return anchor, &ExitError{2, "Error: --path must be a file path relative to the repository root, such as src/main.go."}
	}
	if line < 1 || line > prCommentMaxNumber {
		return anchor, &ExitError{2, "Error: --line must be a positive line number."}
	}
	if rangeGiven {
		if startLine < 1 || startLine > line {
			return anchor, &ExitError{2, "Error: --start-line must be between 1 and --line."}
		}
		anchor.StartLine = startLine
	}
	switch strings.ToLower(side) {
	case "right":
		anchor.Side = "RIGHT"
	case "left":
		anchor.Side = "LEFT"
	default:
		return anchor, &ExitError{2, "Error: invalid --side: " + output.CellText(side) + " (choose right or left)"}
	}
	return anchor, nil
}

// The API rejects absolute paths and .. segments. Empty and . segments are
// accepted there but never match a diff path, so the comment could not be placed.
func validReviewPath(path string) bool {
	if path == "" || !utf8.ValidString(path) || utf8.RuneCountInString(path) > prCommentPathLimit {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func prCommentBody(cmd *cobra.Command, d Dependencies, text, file string, limit int, kind string) (string, error) {
	f := cmd.Flags()
	if f.Changed("body") == f.Changed("body-file") {
		return "", &ExitError{2, "Error: Pass exactly one of --body or --body-file."}
	}
	tooLong := &ExitError{2, fmt.Sprintf("Error: The comment body exceeds the %d-character limit for %s.", limit, kind)}
	if f.Changed("body-file") {
		reader := d.In
		if file != "-" {
			handle, err := os.Open(file)
			if err != nil {
				return "", fmt.Errorf("could not read --body-file: %w", err)
			}
			defer func() { _ = handle.Close() }()
			reader = handle
		}
		maxBytes := limit * utf8.UTFMax
		data, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes)+1))
		if err != nil {
			return "", fmt.Errorf("could not read --body-file: %w", err)
		}
		if len(data) > maxBytes {
			return "", tooLong
		}
		text = string(data)
	}
	switch {
	case !utf8.ValidString(text):
		return "", &ExitError{2, "Error: The comment body must be UTF-8 text."}
	// PostgreSQL text cannot hold NUL, so the server would fail with a 5xx that
	// reads as a possibly committed write.
	case strings.ContainsRune(text, 0):
		return "", &ExitError{2, "Error: The comment body cannot contain NUL characters."}
	case strings.TrimSpace(text) == "":
		return "", &ExitError{2, "Error: The comment body is empty."}
	case utf8.RuneCountInString(text) > limit:
		return "", tooLong
	}
	return text, nil
}

// These routes take no idempotency key. Only a client error proves nothing was
// written; after an interruption, a lost or unreadable response or a server
// error, a blind retry may post the comment twice.
func prCommentError(cfg config.Config, err error, goal string) error {
	const hint = "\n  Hint: the comment may have been created. Check the pull request before retrying."
	if errors.Is(err, context.Canceled) {
		return &ExitError{130, "Interrupted." + hint}
	}
	result := prRequestError(cfg, err, goal, goal)
	var response *client.HTTPError
	if errors.As(err, &response) && response.Status < 500 {
		return result
	}
	return fmt.Errorf("%w%s", result, hint)
}

func prCommentRecord(comment map[string]any, number int, repository string) map[string]any {
	return map[string]any{
		"id": comment["id"], "pull_number": number, "repository_id": repository,
		"body": comment["body"], "publication": comment["publication"], "created_at": comment["created_at"],
	}
}

func prCommentSummary(label string, number int, record map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Commented on %s#%d", output.CellText(label), number)
	if record["thread_id"] != nil {
		fmt.Fprintf(&b, " at %s:", output.CellText(fieldText(record, "path")))
		if record["start_line"] != record["end_line"] {
			fmt.Fprintf(&b, "%v-", record["start_line"])
		}
		fmt.Fprintf(&b, "%v", record["end_line"])
		if record["side"] == "LEFT" {
			b.WriteString(", left side")
		}
		fmt.Fprintf(&b, " (%s, thread %s). ", output.CellText(fieldText(record, "id")), output.CellText(fieldText(record, "thread_id")))
	} else {
		fmt.Fprintf(&b, " (%s). ", output.CellText(fieldText(record, "id")))
	}
	publication, _ := record["publication"].(map[string]any)
	if publication == nil {
		b.WriteString("Avrea did not queue it for GitHub, so it is visible in Avrea only.")
		return b.String()
	}
	switch status := fieldText(publication, "status"); status {
	case "pending":
		b.WriteString("Avrea will publish it to GitHub.")
	case "synced":
		b.WriteString("Avrea published it to GitHub.")
	case "failed":
		b.WriteString("Publishing to GitHub failed")
		if reason := fieldText(publication, "error"); reason != "" {
			b.WriteString(": " + output.CellText(reason))
		}
	default:
		b.WriteString("GitHub publication status: " + output.CellText(status))
	}
	return b.String()
}
