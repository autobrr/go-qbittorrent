# go-qbittorrent

A Go client library for the qBittorrent Web API, with recorded qBittorrent data as testdata.

## Language

**Action**:
One operation of the Web API, written as `controller/action`, for example `torrents/info`.
_Avoid_: Endpoint, method

**Release**:
One qBittorrent version, identified by its source tag.
_Avoid_: Build, image

**Required parameter**:
A parameter that an action rejects the request without.
_Avoid_: Mandatory argument

**Capture**:
A testdata file that the capture command writes from one release, from its Docker image or its source tag.
_Avoid_: Fixture, snapshot

**Sync snapshot**:
The torrents of one instance as one sync of a sync manager left them. Every reader of that sync shares them, and no reader changes them.
_Avoid_: Copy, sync cache, snapshot alone
