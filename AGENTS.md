# AGENTS.md

Repo rules for AI agents working on go-qbittorrent. This is a Go client library
for the qBittorrent WebAPI. There is no app, no database and no frontend here.

## Tests

- CI runs `go test -tags ci ./...`, then `go test ./...` in `internal/capture/`.
  Those two runs are the test gate.
- `methods_test.go` is `//go:build !ci`, so a plain `go test ./...` runs more than
  CI does. A green local run does not prove a green CI run.
- `filter_live_bench_test.go` is `//go:build integration` and needs a live
  qBittorrent. Runnable benchmarks are in `sync_bench_test.go` and `sorting_test.go`.
- A test must never reach a real tracker or a real qBittorrent. Use `httptest`.
- The JSON files in `testdata/preferences/`, `testdata/torrents/` and `testdata/features/`
  are real qBittorrent responses, recorded by the `capture.sh` in each folder from local
  Docker images. Change them only through the script: for a new qBittorrent release, add
  its image to each script and run the script again.
- The JSON files in `testdata/webapi/` come from the qBittorrent source, not from a
  Docker image. Change them only through `go run . webapi` in `internal/capture/`.
  Set `QBT_SRC` to a local qBittorrent clone, or the command clones the source itself.
- `internal/capture/` is a separate Go module, so the root `go test ./...` skips it.
  Keep its dependencies out of the root `go.mod`.

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
- Put AI attribution only in the AI disclosure section of the template, and
  answer that section truthfully.
