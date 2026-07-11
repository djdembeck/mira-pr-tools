# mira-pr-tools

Programmatic tools for posting feedback to [Mira](https://github.com/mira-reviewer/mira) PR review threads — reject false positives, acknowledge valid findings, and close the review learning loop.

## Why

Mira's review bot posts comments on PRs, but there's no programmatic way for AI agent workflows (like `/fixup`) to communicate back which findings were valid and which were false positives. This repo provides scripts that post `@bot-name reject — <reason>` replies on false positive threads and acknowledgments on valid ones, feeding Mira's learning loop without requiring an LLM for each interaction.

## Status

Active — two standalone Go binaries, zero external Go dependencies.

## Build

```bash
make build
```

Produces `bin/mira-review-parser` and `bin/mira-review-reply`.

## Binaries

### mira-review-parser

Fetches PR review comments from GitHub or Forgejo, filters to Mira bot root
comments, and parses them into structured data.

```
mira-review-parser <pr-number> [--format json|consensus] [--include-resolved]
```

- `--format json` (default): `ParsedComment[]` JSON array
- `--format consensus`: markdown summary grouped by file (alphabetically sorted)
- `--include-resolved`: include resolved threads (default: open only)

JSON output schema — every field always present:

```json
{
  "id": "3564917980",
  "file": "src/auth.ts",
  "lineStart": 42,
  "lineEnd": 45,
  "category": "Bug",
  "severity": "blocker",
  "title": "Missing null check",
  "body": "...",
  "suggestion": null,
  "agentPrompt": null,
  "diffHunk": null,
  "isResolved": false,
  "createdAt": "2026-07-11T...",
  "threadReplies": 0
}
```

### mira-review-reply

Posts `reject` or `acknowledge` replies on Mira review comment threads.

```
# Reject a single false positive
mira-review-reply <pr-number> --reject <comment-id> --reason "..."

# Acknowledge a valid finding
mira-review-reply <pr-number> --acknowledge <comment-id> [--commit <hash>] [--note <text>]

# Batch operations
mira-review-reply <pr-number> --batch-reject <file.json>
mira-review-reply <pr-number> --batch-acknowledge <file.json>

# Detect the bot name from PR comments
mira-review-reply <pr-number> --detect-bot
```

Options: `--bot-name <name>`, `--commit <hash>`, `--note <text>`, `--dry-run`,
`--format json`, `--help`.

Batch JSON format:

```json
// --batch-reject
[{ "id": "3564917980", "reason": "Auth at middleware layer" }]

// --batch-acknowledge
[{ "id": "3564917980", "note": "Fixed in commit abc123" }]
```

## Platform detection

Both binaries auto-detect the platform from `git remote get-url origin`:

- `github.com` → GitHub (uses `gh` CLI)
- `git.theiahd.nl` → Forgejo (uses `FORGEJO_URL` + `FORGEJO_TOKEN`)

## Prerequisites

- `git`
- `gh` CLI (for GitHub repos) — handles auth, token refresh, API quirks
- `FORGEJO_URL` and `FORGEJO_TOKEN` env vars (for Forgejo repos)
