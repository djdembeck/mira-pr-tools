package mira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// forgejoRequest performs an HTTP request against the Forgejo API using the
// FORGEJO_URL and FORGEJO_TOKEN env vars. method is the HTTP verb, endpoint is
// the path after /api/v1/. body may be nil for GET requests.
func forgejoRequest(method, endpoint string, body []byte) (string, error) {
	baseURL := os.Getenv("FORGEJO_URL")
	token := os.Getenv("FORGEJO_TOKEN")
	if baseURL == "" || token == "" {
		return "", fmt.Errorf("FORGEJO_URL and FORGEJO_TOKEN must be set for Forgejo repos")
	}

	u, err := url.JoinPath(baseURL, "/api/v1/", endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid Forgejo endpoint: %w", err)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, reader)
	if err != nil {
		return "", fmt.Errorf("build Forgejo request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Forgejo API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read Forgejo response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e errResp
		msg := strings.TrimSpace(string(respBody))
		if json.Unmarshal(respBody, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		return "", fmt.Errorf("Forgejo API error (%d): %s", resp.StatusCode, msg)
	}
	return string(respBody), nil
}

// forgejoGet is a convenience GET wrapper.
func forgejoGet(endpoint string) (string, error) {
	return forgejoRequest(http.MethodGet, endpoint, nil)
}

// forgejoPost is a convenience POST wrapper.
func forgejoPost(endpoint string, body []byte) (string, error) {
	return forgejoRequest(http.MethodPost, endpoint, body)
}

// FetchForgejoComments fetches all review comments for the given PR by
// iterating over reviews and their comment subsets, normalizing each into a
// RawComment.
func FetchForgejoComments(owner, repo string, prNumber int) ([]RawComment, error) {
	reviewsRaw, err := forgejoGet(fmt.Sprintf("repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber))
	if err != nil {
		return nil, err
	}
	var reviews []forgejoReview
	if err := json.Unmarshal([]byte(reviewsRaw), &reviews); err != nil {
		return nil, fmt.Errorf("parse Forgejo reviews: %w", err)
	}

	allComments := make([]RawComment, 0)
	for _, review := range reviews {
		reviewID := review.ID.String()
		if reviewID == "" {
			continue
		}
		commentsRaw, err := forgejoGet(fmt.Sprintf("repos/%s/%s/pulls/%d/reviews/%s/comments", owner, repo, prNumber, reviewID))
		if err != nil {
			return nil, err
		}
		var comments []forgejoComment
		if err := json.Unmarshal([]byte(commentsRaw), &comments); err != nil {
			return nil, fmt.Errorf("parse Forgejo review comments: %w", err)
		}
		for _, c := range comments {
			diffHunk := c.DiffHunk
			if diffHunk == nil {
				diffHunk = c.Diff
			}
			var replyToID *string
			if c.InReplyToID != nil {
				s := fmt.Sprintf("%d", *c.InReplyToID)
				replyToID = &s
			}
			id := c.ID.String()
			createdAt := c.CreatedAt
			allComments = append(allComments, RawComment{
				ID:         id,
				Body:       c.Body,
				Path:       c.Path,
				Line:       c.Line,
				StartLine:  c.StartLine,
				DiffHunk:   diffHunk,
				Author:     c.User.Login,
				CreatedAt:  createdAt,
				IsResolved: c.Resolved,
				IsOutdated: false,
				ReplyToID:  replyToID,
			})
		}
	}
	return allComments, nil
}

// PostForgejoReply posts a reply to a Forgejo review comment via the REST API.
// When dryRun is true no request is made.
func PostForgejoReply(owner, repo string, prNumber int, commentID, body string, dryRun bool) (ReplyResult, error) {
	if dryRun {
		return ReplyResult{Success: true}, nil
	}
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return ReplyResult{Success: false, Error: err.Error()}, nil
	}
	output, err := forgejoPost(fmt.Sprintf("repos/%s/%s/pulls/%d/comments/%s/replies", owner, repo, prNumber, commentID), payload)
	if err != nil {
		return ReplyResult{Success: false, Error: err.Error()}, nil
	}
	var resp struct {
		URL string `json:"url"`
	}
	if jsonErr := json.Unmarshal([]byte(output), &resp); jsonErr == nil && resp.URL != "" {
		return ReplyResult{Success: true, ReplyURL: resp.URL}, nil
	}
	return ReplyResult{Success: true}, nil
}

// DetectForgejoBotName inspects the PR's review authors for a Mira bot and
// returns the first match, falling back to "miracodeai-bot".
func DetectForgejoBotName(owner, repo string, prNumber int) (string, error) {
	reviewsRaw, err := forgejoGet(fmt.Sprintf("repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber))
	if err != nil {
		return "miracodeai-bot", nil
	}
	var reviews []forgejoReview
	if err := json.Unmarshal([]byte(reviewsRaw), &reviews); err != nil {
		return "miracodeai-bot", nil
	}
	for _, review := range reviews {
		if IsMiraComment(review.User.Login) {
			return review.User.Login, nil
		}
	}
	return "miracodeai-bot", nil
}
