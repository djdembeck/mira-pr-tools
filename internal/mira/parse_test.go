package mira

import (
	"testing"
)

// pstr returns a pointer to s for constructing RawComment values inline.
func pstr(s string) *string { return &s }

// TestFilterMiraRootCommentsNoExtras locks the current behavior: with no
// additional authors, only Mira-authored root comments are kept.
func TestFilterMiraRootCommentsNoExtras(t *testing.T) {
	comments := []RawComment{
		{ID: "1", Author: "miracodeai-bot", ReplyToID: nil},
		{ID: "2", Author: "nimuebot", ReplyToID: nil},
		{ID: "3", Author: "miracodeai", ReplyToID: pstr("1")},
		{ID: "4", Author: "human", ReplyToID: nil},
	}
	got := FilterMiraRootComments(comments)
	if len(got) != 1 {
		t.Fatalf("expected 1 root comment, got %d", len(got))
	}
	if got[0].ID != "1" {
		t.Fatalf("expected comment 1, got %s", got[0].ID)
	}
}

// TestFilterMiraRootCommentsWithAdditionalAuthors verifies that an additional
// author augments the Mira filter and that isMira is scoped per comment.
func TestFilterMiraRootCommentsWithAdditionalAuthors(t *testing.T) {
	comments := []RawComment{
		{ID: "1", Author: "miracodeai-bot", ReplyToID: nil},
		{ID: "2", Author: "nimuebot", ReplyToID: nil},
		{ID: "3", Author: "nimuebot", ReplyToID: pstr("2")},
		{ID: "4", Author: "human", ReplyToID: nil},
	}
	got := FilterMiraRootComments(comments, "nimuebot")
	if len(got) != 2 {
		t.Fatalf("expected 2 root comments, got %d", len(got))
	}
	byID := map[string]RawComment{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if _, ok := byID["1"]; !ok {
		t.Fatal("expected Mira comment 1 kept")
	}
	if _, ok := byID["2"]; !ok {
		t.Fatal("expected nimuebot comment 2 kept")
	}
	if _, ok := byID["3"]; ok {
		t.Fatal("reply comment 3 must never be kept")
	}
	if _, ok := byID["4"]; ok {
		t.Fatal("unrelated human comment 4 must not be kept")
	}

	miraParsed := ParseMiraComment(byID["1"])
	if !miraParsed.IsMira || miraParsed.Author != "miracodeai-bot" {
		t.Fatalf("Mira comment: want Author=miracodeai-bot IsMira=true, got Author=%q IsMira=%v", miraParsed.Author, miraParsed.IsMira)
	}
	nebParsed := ParseMiraComment(byID["2"])
	if nebParsed.IsMira || nebParsed.Author != "nimuebot" {
		t.Fatalf("nimuebot comment: want Author=nimuebot IsMira=false, got Author=%q IsMira=%v", nebParsed.Author, nebParsed.IsMira)
	}
}

// TestFilterMiraRootCommentsExcludesReplies verifies replies are never kept
// regardless of author, additional or otherwise.
func TestFilterMiraRootCommentsExcludesReplies(t *testing.T) {
	comments := []RawComment{
		{ID: "1", Author: "miracodeai-bot", ReplyToID: pstr("0")},
		{ID: "2", Author: "nimuebot", ReplyToID: pstr("0")},
	}
	got := FilterMiraRootComments(comments, "nimuebot")
	if len(got) != 0 {
		t.Fatalf("expected 0 comments, got %d", len(got))
	}
}

// TestFilterMiraRootCommentsMatchingCases verifies matching is case-insensitive,
// trims spaces around CSV entries, and non-matching additional authors change
// nothing.
func TestFilterMiraRootCommentsMatchingCases(t *testing.T) {
	comments := []RawComment{
		{ID: "1", Author: "miracodeai-bot", ReplyToID: nil},
		{ID: "2", Author: "NimueBot", ReplyToID: nil},
		{ID: "3", Author: "otherreviewer", ReplyToID: nil},
	}

	// main.go splits --additional-authors CSV into individual logins before
	// calling the filter, so this test passes already-split entries that are
	// case-mismatched, whitespace-padded, or non-matching.
	got := FilterMiraRootComments(comments, "nimbot", " NimueBot ", "other-reviewer")
	wantIDs := map[string]bool{"1": true, "2": true}
	if len(got) != len(wantIDs) {
		t.Fatalf("expected %d comments, got %d", len(wantIDs), len(got))
	}
	for _, c := range got {
		if !wantIDs[c.ID] {
			t.Fatalf("unexpected comment %s kept", c.ID)
		}
	}
}
