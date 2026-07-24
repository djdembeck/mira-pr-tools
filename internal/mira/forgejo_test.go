package mira

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// setTestEnv overrides the forgejoEnv closure for the duration of the test.
// Thread-safe — no data race with concurrent tests or production code.
func setTestEnv(t *testing.T, srv *httptest.Server) {
	t.Helper()
	forgejoEnvMu.Lock()
	orig := forgejoEnv
	forgejoEnv = func() (string, string) {
		return srv.URL, "test-token"
	}
	forgejoEnvMu.Unlock()
	t.Cleanup(func() {
		forgejoEnvMu.Lock()
		forgejoEnv = orig
		forgejoEnvMu.Unlock()
	})
}

// captureError stores the first error message and is a no-op after the first
// call. Safe for concurrent use from httptest handlers.
type captureError struct {
	mu  sync.Mutex
	hit bool
	msg string
}

func (c *captureError) fatal(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hit {
		c.msg = fmt.Sprintf(format, args...)
		c.hit = true
	}
}

func (c *captureError) check(t *testing.T) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hit {
		t.Fatalf("handler error: %s", c.msg)
	}
}

// TestForgejoTryReplySucceedsOnFirstAttempt verifies strategy 1 (comments/{id}/replies)
// returns a ReplyResult with the URL extracted from the JSON response.
func TestForgejoTryReplySucceedsOnFirstAttempt(t *testing.T) {
	var err captureError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			err.fatal("expected POST, got %s", r.Method)
			http.Error(w, "bad method", http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/api/v1/repos/owner/repo/pulls/42/comments/100/replies" {
			err.fatal("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"url": "https://example.com/reply/999"})
	}))
	defer srv.Close()

	setTestEnv(t, srv)

	result, testErr := forgejoTryReply("owner", "repo", 42, "100", "test reply", "50")
	err.check(t)
	if testErr != nil {
		t.Fatalf("unexpected error: %v", testErr)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.ReplyURL != "https://example.com/reply/999" {
		t.Fatalf("expected url https://example.com/reply/999, got %q", result.ReplyURL)
	}
}

// TestForgejoTryReplyFallsBackToRootID verifies that when strategy 1 returns
// 405, strategy 2 (/reviews/{id}/comments with root_id) is tried and succeeds.
func TestForgejoTryReplyFallsBackToRootID(t *testing.T) {
	var err captureError
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		switch r.URL.Path {
		case "/api/v1/repos/owner/repo/pulls/42/comments/100/replies":
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"method not allowed"}`))
		case "/api/v1/repos/owner/repo/pulls/42/reviews/50/comments":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["root_id"] == nil {
				err.fatal("expected root_id in payload for strategy 2")
				http.Error(w, "missing root_id", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"url": "https://example.com/reply/root"})
		default:
			err.fatal("unexpected path on attempt %d: %s", attempts, r.URL.Path)
			http.Error(w, "bad path", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	setTestEnv(t, srv)

	result, testErr := forgejoTryReply("owner", "repo", 42, "100", "test reply", "50")
	err.check(t)
	if testErr != nil {
		t.Fatalf("unexpected error: %v", testErr)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.ReplyURL != "https://example.com/reply/root" {
		t.Fatalf("expected url https://example.com/reply/root, got %q", result.ReplyURL)
	}
}

// TestForgejoTryReplyFallsBackToIssueComment verifies that when all three
// review endpoints fail, strategy 4 (/issues/{pr}/comments) is tried and succeeds.
func TestForgejoTryReplyFallsBackToIssueComment(t *testing.T) {
	// Track which endpoints were actually called for the failing strategies.
	var repliesHit, reviewsWithRootHit, reviewsWithoutRootHit atomic.Bool
	var err captureError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/owner/repo/pulls/42/comments/100/replies":
			repliesHit.Store(true)
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		case "/api/v1/repos/owner/repo/pulls/42/reviews/50/comments":
			// Strategy 2 and 3 both hit this path; distinguish by body.
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["root_id"] != nil {
				reviewsWithRootHit.Store(true)
			} else {
				reviewsWithoutRootHit.Store(true)
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		case "/api/v1/repos/owner/repo/issues/42/comments":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"html_url": "https://example.com/issue/1"})
		default:
			err.fatal("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	setTestEnv(t, srv)

	result, testErr := forgejoTryReply("owner", "repo", 42, "100", "test reply", "50")
	err.check(t)
	if testErr != nil {
		t.Fatalf("unexpected error: %v", testErr)
	}

	// Verify all fallback endpoints were hit.
	if !repliesHit.Load() {
		t.Fatal("strategy 1 (comments/replies) was not attempted")
	}
	if !reviewsWithRootHit.Load() {
		t.Fatal("strategy 2 (reviews/comments with root_id) was not attempted")
	}
	if !reviewsWithoutRootHit.Load() {
		t.Fatal("strategy 3 (reviews/comments without root_id) was not attempted")
	}

	if !result.Success {
		t.Fatalf("expected success, got Success=%v Error=%q", result.Success, result.Error)
	}
	if result.ReplyURL != "https://example.com/issue/1" {
		t.Fatalf("expected url https://example.com/issue/1, got %q", result.ReplyURL)
	}
}

// TestParseForgejoReplyResponseWithURL verifies URL extraction from JSON.
func TestParseForgejoReplyResponseWithURL(t *testing.T) {
	result, err := parseForgejoReplyResponse(`{"url":"https://example.com/reply/42"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.ReplyURL != "https://example.com/reply/42" {
		t.Fatalf("expected url https://example.com/reply/42, got %q", result.ReplyURL)
	}
}

// TestParseForgejoReplyResponseNoURL verifies Success:true with empty URL
// when the JSON has no url field.
func TestParseForgejoReplyResponseNoURL(t *testing.T) {
	result, err := parseForgejoReplyResponse(`{"html_url":"https://example.com/other"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.ReplyURL != "" {
		t.Fatalf("expected empty url, got %q", result.ReplyURL)
	}
}

// TestPostForgejoReplyDryRun verifies that dryRun=true returns immediately
// without making any HTTP request.
func TestPostForgejoReplyDryRun(t *testing.T) {
	requested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
	}))
	defer srv.Close()

	setTestEnv(t, srv)

	dryRun := true
	result, err := PostForgejoReply("owner", "repo", 42, "100", "body", dryRun)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success on dry run")
	}
	if requested {
		t.Fatal("dry run should not make any HTTP request")
	}
}

// TestFindForgejoReviewIDForCommentFound verifies the review ID is returned
// when the comment exists in a review's comment list.
func TestFindForgejoReviewIDForCommentFound(t *testing.T) {
	var err captureError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/repos/owner/repo/pulls/42/reviews":
			_ = json.NewEncoder(w).Encode([]forgejoReview{
				{ID: json.Number("7"), User: forgejoUser{Login: "u1"}},
				{ID: json.Number("50"), User: forgejoUser{Login: "u2"}},
			})
		case "/api/v1/repos/owner/repo/pulls/42/reviews/7/comments":
			_ = json.NewEncoder(w).Encode([]forgejoComment{
				{ID: json.Number("99"), Body: "other", User: forgejoUser{Login: "u1"}},
			})
		case "/api/v1/repos/owner/repo/pulls/42/reviews/50/comments":
			_ = json.NewEncoder(w).Encode([]forgejoComment{
				{ID: json.Number("100"), Body: "target", User: forgejoUser{Login: "u2"}},
			})
		default:
			err.fatal("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	setTestEnv(t, srv)

	id, testErr := findForgejoReviewIDForComment("owner", "repo", 42, "100")
	err.check(t)
	if testErr != nil {
		t.Fatalf("unexpected error: %v", testErr)
	}
	if id != "50" {
		t.Fatalf("expected review ID 50, got %q", id)
	}
}

// TestFindForgejoReviewIDForCommentNotFound verifies an error is returned
// when the comment ID does not exist in any review.
func TestFindForgejoReviewIDForCommentNotFound(t *testing.T) {
	var err captureError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/repos/owner/repo/pulls/42/reviews":
			_ = json.NewEncoder(w).Encode([]forgejoReview{
				{ID: json.Number("1"), User: forgejoUser{Login: "u1"}},
			})
		case "/api/v1/repos/owner/repo/pulls/42/reviews/1/comments":
			_ = json.NewEncoder(w).Encode([]forgejoComment{
				{ID: json.Number("200"), Body: "no", User: forgejoUser{Login: "u1"}},
			})
		default:
			err.fatal("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	setTestEnv(t, srv)

	_, testErr := findForgejoReviewIDForComment("owner", "repo", 42, "9999")
	err.check(t)
	if testErr == nil {
		t.Fatal("expected error when comment not found")
	}
	if !strings.Contains(testErr.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", testErr)
	}
}
