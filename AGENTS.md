# AGENTS.md

Repo rules for AI agents working on go-qbittorrent. This is a Go client library
for the qBittorrent WebAPI. There is no app, no database and no frontend here.

## Tests

- CI runs `go test -tags ci ./...`. Match it before you report a change complete.
- `methods_test.go` is `//go:build !ci`, so a plain `go test ./...` runs more than
  CI does. A green local run does not prove a green CI run.
- `filter_live_bench_test.go` is `//go:build integration` and needs a live
  qBittorrent. Runnable benchmarks are in `sync_bench_test.go` and `sorting_test.go`.
- A test must never reach a real tracker or a real qBittorrent. Use `httptest`.

## Generated code

Both generators in `internal/codegen/` read struct definitions from `domain.go`
only. Run `go generate` after you change one. `codegen_test.go` carries no build
tag, so CI fails when a generated file is stale.

## Downstream

This library is public, so an exported API change breaks callers at compile time,
including callers outside the autobrr org. Three projects in the org are the main
consumers, so say in the PR which of them a change reaches:

- **qui** uses the sync layer, the client pool and the view and filter types, so
  it feels almost any change here.
- **autobrr** uses the plain client: add, reannounce, filters, transfer info.
- **librrary** uses the plain client. The repo is private.

## Pull requests

- Base branch is `main`. Conventional commits; the PR title becomes the squashed
  commit message.
- Fill `.github/pull_request_template.md` into the body. `gh pr create --body`
  does not do it for you.
- Update a PR by adding a commit. Never force-push: PRs are squash-merged, so a
  rewrite gains nothing and breaks review history.
- Answer the AI disclosure section truthfully. Never put AI attribution in a
  commit message or a PR body.
