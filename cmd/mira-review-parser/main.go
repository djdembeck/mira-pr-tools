// Command mira-review-parser fetches PR review comments from GitHub or
// Forgejo, filters to Mira bot root comments, and parses them into structured
// data (JSON or consensus markdown).
//
// Usage:
//
//	mira-review-parser <pr-number> [--format json|consensus] [--include-resolved]
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/djdembeck/mira-pr-tools/internal/mira"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: mira-review-parser <pr-number> [--format json|consensus] [--include-resolved]")
		os.Exit(1)
	}

	prNumber, err := strconv.Atoi(args[0])
	if err != nil || prNumber <= 0 {
		fmt.Fprintf(os.Stderr, "Invalid PR number: %s\n", args[0])
		os.Exit(1)
	}

	format := "json"
	includeResolved := false

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 < len(args) {
				f := args[i+1]
				if f == "json" || f == "consensus" {
					format = f
					i++
				} else {
					fmt.Fprintf(os.Stderr, "Error: --format must be 'json' or 'consensus', got '%s'\n", f)
					os.Exit(1)
				}
			} else {
				fmt.Fprintln(os.Stderr, "Error: --format requires a value")
				os.Exit(1)
			}
		case "--include-resolved":
			includeResolved = true
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown flag '%s'\n", args[i])
			os.Exit(1)
		}
	}

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

	fmt.Fprintf(os.Stderr, "- Detecting %s repo: %s/%s\n", platform, owner, repo)

	var rawComments []mira.RawComment
	switch platform {
	case mira.PlatformGitHub:
		rawComments, err = mira.FetchGitHubComments(owner, repo, prNumber)
	case mira.PlatformForgejo:
		rawComments, err = mira.FetchForgejoComments(owner, repo, prNumber)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching comments: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "- Fetched %d total review comments\n", len(rawComments))

	miraRoots := mira.FilterMiraRootComments(rawComments)
	fmt.Fprintf(os.Stderr, "- Found %d Mira root comments\n", len(miraRoots))

	parsed := make([]mira.ParsedComment, 0, len(miraRoots))
	for _, c := range miraRoots {
		parsed = append(parsed, mira.ParseMiraComment(c))
	}

	if !includeResolved {
		open := make([]mira.ParsedComment, 0, len(parsed))
		for _, c := range parsed {
			if !c.IsResolved {
				open = append(open, c)
			}
		}
		parsed = open
		fmt.Fprintf(os.Stderr, "- After filtering resolved: %d open comments\n", len(parsed))
	}

	var output string
	if format == "consensus" {
		output = mira.FormatConsensus(parsed)
	} else {
		output, err = mira.FormatJSON(parsed)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println(output)
}
