package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/djdembeck/mira-pr-tools/internal/mira"
)

type action string

const (
	actionReject           action = "reject"
	actionAcknowledge      action = "acknowledge"
	actionBatchReject      action = "batch-reject"
	actionBatchAcknowledge action = "batch-acknowledge"
	actionDetectBot        action = "detect-bot"
)

type args struct {
	prNumber   int
	act        action
	commentID  string
	reason     string
	commitHash string
	note       string
	batchFile  string
	botName    string
	dryRun     bool
	formatJSON bool
}

func isHelp(s string) bool {
	return s == "--help" || s == "-h"
}

type rejectEntry struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type acknowledgeEntry struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `mira-review-reply — Post feedback replies to Mira review threads

Usage:
  mira-review-reply <pr-number> --reject <comment-id> --reason "..."
  mira-review-reply <pr-number> --acknowledge <comment-id> [--commit abc123]
  mira-review-reply <pr-number> --batch-reject <file.json>
  mira-review-reply <pr-number> --batch-acknowledge <file.json>
  mira-review-reply <pr-number> --detect-bot

Options:
  --bot-name <name>   Override bot name (default: auto-detect)
  --commit <hash>     Include commit hash in acknowledgment
  --note <text>       Append a note to the acknowledgment
  --dry-run           Print actions without posting
  --format json       Output results as JSON
  --help              Show this help`)
}

func parseArgs(argv []string) args {
	a := args{}
	argsList := argv[1:]
	if len(argsList) == 0 {
		printUsage()
		os.Exit(1)
	}
	// --help / -h may appear as the first arg without a PR number.
	if isHelp(argsList[0]) {
		printUsage()
		os.Exit(0)
	}

	prNumber, err := strconv.Atoi(argsList[0])
	if err != nil || prNumber <= 0 {
		fmt.Fprintf(os.Stderr, "Invalid PR number: %s\n", argsList[0])
		os.Exit(1)
	}
	a.prNumber = prNumber

	for i := 1; i < len(argsList); i++ {
		arg := argsList[i]
		switch arg {
		case "--reject":
			a.act = actionReject
			i++
			if i < len(argsList) {
				a.commentID = argsList[i]
			}
		case "--acknowledge", "--ack":
			a.act = actionAcknowledge
			i++
			if i < len(argsList) {
				a.commentID = argsList[i]
			}
		case "--batch-reject":
			a.act = actionBatchReject
			i++
			if i < len(argsList) {
				a.batchFile = argsList[i]
			}
		case "--batch-acknowledge", "--batch-ack":
			a.act = actionBatchAcknowledge
			i++
			if i < len(argsList) {
				a.batchFile = argsList[i]
			}
		case "--reason":
			i++
			if i < len(argsList) {
				a.reason = argsList[i]
			}
		case "--commit":
			i++
			if i < len(argsList) {
				a.commitHash = argsList[i]
			}
		case "--note":
			i++
			if i < len(argsList) {
				a.note = argsList[i]
			}
		case "--bot-name":
			i++
			if i < len(argsList) {
				a.botName = argsList[i]
			}
		case "--detect-bot":
			a.act = actionDetectBot
		case "--dry-run":
			a.dryRun = true
		case "--format":
			i++
			if i < len(argsList) && argsList[i] == "json" {
				a.formatJSON = true
			}
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "Unknown argument: %s\n", arg)
			printUsage()
			os.Exit(1)
		}
	}

	if a.act == "" {
		fmt.Fprintln(os.Stderr, "Error: must specify --reject, --acknowledge, --batch-reject, --batch-acknowledge, or --detect-bot")
		printUsage()
		os.Exit(1)
	}
	if (a.act == actionReject || a.act == actionAcknowledge) && a.commentID == "" {
		fmt.Fprintf(os.Stderr, "Error: --%s requires a comment ID\n", a.act)
		os.Exit(1)
	}
	if a.act == actionReject && a.reason == "" {
		fmt.Fprintln(os.Stderr, "Error: --reject requires --reason")
		os.Exit(1)
	}
	if (a.act == actionBatchReject || a.act == actionBatchAcknowledge) && a.batchFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --%s requires a file path\n", a.act)
		os.Exit(1)
	}
	return a
}

// readBatchFile reads a JSON array from path. It accepts raw arrays of
// `{id,reason}` or `{id,note}` objects.
func readBatchFile(path string, out interface{}) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading batch file: %v\n", err)
		os.Exit(1)
	}
	var arr json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: batch file must contain a JSON array: %s\n", path)
		os.Exit(1)
	}
	// arr is a raw JSON value; check it's an array by inspecting the first
	// non-whitespace byte.
	trimmed := strings.TrimSpace(string(arr))
	if !strings.HasPrefix(trimmed, "[") {
		fmt.Fprintf(os.Stderr, "Error: batch file must contain a JSON array: %s\n", path)
		os.Exit(1)
	}
	if err := json.Unmarshal(data, out); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing batch file: %v\n", err)
		os.Exit(1)
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func main() {
	opts := parseArgs(os.Args)

	remote, err := mira.GetGitRemote()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	platform := mira.DetectPlatform(remote)
	owner, repo, err := mira.ParseRemoteRepo(remote)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "- Platform: %s, Repo: %s/%s, PR: %d\n", platform, owner, repo, opts.prNumber)

	// Detect or use provided bot name.
	botName := opts.botName
	if botName == "" {
		switch platform {
		case mira.PlatformGitHub:
			botName, _ = mira.DetectGitHubBotName(owner, repo, opts.prNumber)
		case mira.PlatformForgejo:
			botName, _ = mira.DetectForgejoBotName(owner, repo, opts.prNumber)
		}
		if opts.act == actionDetectBot {
			fmt.Println(botName)
			return
		}
		fmt.Fprintf(os.Stderr, "- Detected bot name: %s\n", botName)
	}

	if opts.act == actionDetectBot {
		fmt.Println(botName)
		return
	}

	postReply := func(cid, body string) mira.ReplyResult {
		if platform == mira.PlatformGitHub {
			res, _ := mira.PostGitHubReply(owner, repo, opts.prNumber, cid, body, opts.dryRun)
			if opts.dryRun {
				fmt.Fprintf(os.Stderr, "  [dry-run] Would reply to comment %s: %s...\n", cid, truncate(body, 80))
			}
			return res
		}
		res, _ := mira.PostForgejoReply(owner, repo, opts.prNumber, cid, body, opts.dryRun)
		if opts.dryRun {
			fmt.Fprintf(os.Stderr, "  [dry-run] Would reply to comment %s: %s...\n", cid, truncate(body, 80))
		}
		return res
	}

	var results []mira.ReplyResult

	switch opts.act {
	case actionReject:
		body := mira.BuildRejectBody(botName, opts.reason, opts.commitHash)
		fmt.Fprintf(os.Stderr, "- Rejecting comment %s: %s...\n", opts.commentID, truncate(opts.reason, 80))
		res := postReply(opts.commentID, body)
		results = append(results, mira.ReplyResult{
			CommentID: opts.commentID,
			Action:    "reject",
			Body:      body,
			Success:   res.Success,
			Error:     res.Error,
			ReplyURL:  res.ReplyURL,
		})

	case actionAcknowledge:
		body := mira.BuildAcknowledgeBody(opts.commitHash, opts.note)
		fmt.Fprintf(os.Stderr, "- Acknowledging comment %s\n", opts.commentID)
		res := postReply(opts.commentID, body)
		results = append(results, mira.ReplyResult{
			CommentID: opts.commentID,
			Action:    "acknowledge",
			Body:      body,
			Success:   res.Success,
			Error:     res.Error,
			ReplyURL:  res.ReplyURL,
		})

	case actionBatchReject:
		var entries []rejectEntry
		readBatchFile(opts.batchFile, &entries)
		fmt.Fprintf(os.Stderr, "- Batch reject: %d comments from %s\n", len(entries), opts.batchFile)
		for _, entry := range entries {
			if entry.ID == "" || entry.Reason == "" {
				results = append(results, mira.ReplyResult{
					CommentID: mira.FallbackID(entry.ID),
					Action:    "reject",
					Body:      "",
					Success:   false,
					Error:     "Missing id or reason in batch entry",
				})
				continue
			}
			body := mira.BuildRejectBody(botName, entry.Reason, opts.commitHash)
			fmt.Fprintf(os.Stderr, "  - Rejecting %s: %s...\n", entry.ID, truncate(entry.Reason, 60))
			res := postReply(entry.ID, body)
			results = append(results, mira.ReplyResult{
				CommentID: entry.ID,
				Action:    "reject",
				Body:      body,
				Success:   res.Success,
				Error:     res.Error,
				ReplyURL:  res.ReplyURL,
			})
		}

	case actionBatchAcknowledge:
		var entries []acknowledgeEntry
		readBatchFile(opts.batchFile, &entries)
		fmt.Fprintf(os.Stderr, "- Batch acknowledge: %d comments from %s\n", len(entries), opts.batchFile)
		for _, entry := range entries {
			if entry.ID == "" {
				results = append(results, mira.ReplyResult{
					CommentID: "?",
					Action:    "acknowledge",
					Body:      "",
					Success:   false,
					Error:     "Missing id in batch entry",
				})
				continue
			}
			body := mira.BuildAcknowledgeBody(opts.commitHash, entry.Note)
			fmt.Fprintf(os.Stderr, "  - Acknowledging %s\n", entry.ID)
			res := postReply(entry.ID, body)
			results = append(results, mira.ReplyResult{
				CommentID: entry.ID,
				Action:    "acknowledge",
				Body:      body,
				Success:   res.Success,
				Error:     res.Error,
				ReplyURL:  res.ReplyURL,
			})
		}
	}

	succeeded := 0
	for _, r := range results {
		if r.Success {
			succeeded++
		}
	}
	failed := len(results) - succeeded

	if opts.formatJSON {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(strings.TrimRight(buf.String(), "\n"))
	} else {
		for _, r := range results {
			if r.Success {
				extra := ""
				if r.ReplyURL != "" {
					extra = " → " + r.ReplyURL
				}
				fmt.Fprintf(os.Stderr, "  ✓ %s %s%s\n", r.Action, r.CommentID, extra)
			} else {
				fmt.Fprintf(os.Stderr, "  ✗ %s %s: %s\n", r.Action, r.CommentID, r.Error)
			}
		}
		fmt.Fprintf(os.Stderr, "\nDone: %d succeeded, %d failed\n", succeeded, failed)
	}

	if failed > 0 {
		os.Exit(1)
	}
}
