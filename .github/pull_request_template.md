## Description

<!--- Summarize the change and the motivation. --->

Fixes # (issue)

## API and behaviour change

<!--- Name each change a caller gets by upgrading, and say what they must do.
      Delete this section if there is none. --->

## Downstream impact

<!--- This must be accurate. These three projects in the autobrr org are the main
      consumers of this library. For each one, say whether the change reaches it, and
      whether it needs a code change or only a version bump. Write "none" where the
      change cannot reach it. Check the code before you claim it. Keep this section.

      qui      — uses the sync layer, the client pool and the view and filter types,
                 so it feels almost any change here.
      autobrr  — uses the plain client: add, reannounce, filters, transfer info.
      librrary — uses the plain client. Private repo, so write "unknown" if you
                 cannot read it.
--->

- **qui**:
- **autobrr**:
- **librrary**:

## How has this been tested?

<!--- Only manual or live verification that CI cannot do: a run against a real
      qBittorrent, the version it ran, and the edge this change targets.
      CI runs `go test -tags ci ./...`, so do not list that here.
      Delete this section if CI covers everything. --->

## Performance

<!--- For a change to the sync layer or to sorting, give before and after
      `go test -bench` output and name the benchmark. A throwaway benchmark is fine
      and you need not commit it. Delete this section if the change touches neither. --->

## Checklist

- [ ] My PR title follows the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#summary) format (it becomes the squashed commit message)

## AI disclosure

<!--- Please do not lie about the AI usage in this PR. --->

Was any AI used in this PR? Roughly how much of the code was AI-written, and what model/tool?
