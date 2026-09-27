## Description

<!--- Summarize the change and the motivation. --->

Fixes # (issue)

## API and behaviour change

<!--- Name each change a caller gets by upgrading, and say what they must do.
      Delete this section if there is none. --->

## Downstream impact

<!--- Optional, but it must be accurate. Say whether this change reaches the projects
      that use this library, and whether each one needs a code change or only a
      version bump. Check the code before you claim it. Delete this section if the
      change cannot reach any of them.

      qui      — uses the sync layer, the client pool and the view and filter types,
                 so it feels almost any change here.
      autobrr  — uses the plain client: add, reannounce, filters, transfer info.
      librrary — uses the plain client. Private repo, so skip it if you cannot read it.
--->

## How has this been tested?

<!--- Only manual or live verification that CI cannot do: a run against a real
      qBittorrent, the version it ran, and the edge this change targets.
      CI runs `go test -tags ci ./...`, so do not list that here.
      Delete this section if CI covers everything. --->

## Performance

<!--- For a change to the sync layer or to sorting, give before and after
      `go test -bench` output and name the benchmark. Delete this section if the
      change touches neither. --->

## Checklist

- [ ] My PR title follows the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#summary) format (it becomes the squashed commit message)

## AI disclosure

<!--- Please do not lie about the AI usage in this PR. --->

Was any AI used in this PR? Roughly how much of the code was AI-written, and what model/tool?
