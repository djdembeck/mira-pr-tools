# mira-pr-tools

Programmatic tools for posting feedback to [Mira](https://github.com/mira-reviewer/mira) PR review threads — reject false positives, acknowledge valid findings, and close the review learning loop.

## Why

Mira's review bot posts comments on PRs, but there's no programmatic way for AI agent workflows (like `/fixup`) to communicate back which findings were valid and which were false positives. This repo provides scripts that post `@bot-name reject — <reason>` replies on false positive threads and acknowledgments on valid ones, feeding Mira's learning loop without requiring an LLM for each interaction.

## Status

Early — repo initialization. Scripts and docs coming soon.
