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
- The JSON files in `testdata/preferences/`, `testdata/torrents/`, `testdata/features/` and
  `testdata/filters/` are real qBittorrent responses, recorded from local Docker images by
  `go run . features`, `go run . preferences`, `go run . torrents` and `go run . filters` in
  `internal/capture/`. CI does not run these commands. Change the files only through the
  command: for a new qBittorrent release, add it to the version list of each subcommand and
  run the subcommand again.
- `filter_results_test.go` fails on each difference between the library filter and
  `testdata/filters/` that it does not name. To accept a new difference, name it there
  with its reason.
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

## Rules and what enforces them

When a review corrects a mistake, add its rule here with what enforces it. When a
rule that only review enforces is broken again, add a check in the same change.

| Rule | Enforced by |
|---|---|
| A function that takes `ctx` passes it on: it calls `XCtx(ctx, ...)`, never `X` or `context.Background()` | `TestLibraryCodePassesTheCallersContext` in `rules_context_test.go` |
| An `httptest` handler reports with `t.Errorf` or `assert`, because it runs on a server goroutine | `TestHTTPHandlersInTestsDoNotFailNow` in `rules_handler_test.go` |
| Every `.go` file is gofmt-formatted; the generators format their own output | `TestGoFilesAreGofmted` in `rules_format_test.go` |
| A generated file matches its generator | `TestAllGeneratedFilesAreUpToDate` in `codegen_test.go` |
| The library filter matches the recorded qBittorrent results, apart from named differences | `TestFilterResultsMatchQBittorrent` in `filter_results_test.go` |
| The recorded WebAPI actions match the qBittorrent source and the feature probes | `TestWebAPIActionsComplete` and `TestWebAPIActionsMatchFeatureProbes` in `webapi_actions_test.go` |
| A test talks only to `httptest`, never to a real tracker or qBittorrent | Review only |
| `testdata/` changes only through the capture commands | Review only |
