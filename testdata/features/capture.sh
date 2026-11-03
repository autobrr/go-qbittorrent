#!/usr/bin/env bash
# Probe the qBittorrent features that qui gates by Web API version, and save one
# JSON file for each release. Tests only read the saved files.
# Requires Docker, curl in the images, jq, mkbrr and Go. qBittorrent runs with no network.
# Usage: testdata/features/capture.sh
set -euo pipefail

cd "$(dirname "$0")"
# The newest linuxserver tag of each stable Web API version from 4.3.9 up.
# 14.3.9 is the linuxserver tag of qBittorrent 4.3.9.
tags=(14.3.9 4.4.0 4.4.5 4.5.0 4.5.5 4.6.0 4.6.7 5.0.4 5.1.4 5.2.4 5.3.0)
scratch=$(mktemp -d)
name=qbt-feature-capture-${scratch##*/}
cleanup() {
	docker rm -f "$name" >/dev/null 2>&1 || true
	rm -rf "$scratch"
}
trap cleanup EXIT

tracker=http://127.0.0.1:1/announce
truncate -s 65536 "$scratch/probe.bin"
nice -n 19 env GOMAXPROCS=2 mkbrr create "$scratch/probe.bin" --workers 1 \
	--piece-length 16 --tracker "$tracker" --no-creator --output "$scratch/probe.torrent" --quiet >/dev/null

url=http://127.0.0.1:8080/api/v2
api() {
	docker exec "$name" curl -fsS --max-time 5 -b /tmp/cookies \
		-H 'Referer: http://127.0.0.1:8080' "$@"
}
# Prints the HTTP status code of the request.
status() {
	docker exec "$name" curl -sS -o /dev/null -w '%{http_code}' --max-time 5 -b /tmp/cookies \
		-H 'Referer: http://127.0.0.1:8080' "$@"
}
# trackers prints announce and announce-list of a .torrent file as JSON.
mkdir "$scratch/trackers"
cat >"$scratch/trackers/main.go" <<'GO'
package main

import (
	"encoding/json"
	"os"

	"github.com/autobrr/go-torrent/metainfo"
)

func main() {
	mi, err := metainfo.LoadFromFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"announce": mi.Announce, "announce_list": mi.AnnounceList,
	}); err != nil {
		panic(err)
	}
}
GO
(cd "$scratch/trackers" && go mod init trackers 2>/dev/null \
	&& go get github.com/autobrr/go-torrent@v1.1.1 2>/dev/null && go build -buildvcs=false -o "$scratch/trackers-bin" .)

# POST-only actions answer GET with 405. Releases before 4.5 run the action on a
# GET and answer 400 for the missing parameters, so nothing changes. An action that does not exist answers 404.
endpoints=(torrents/setTags torrents/setComment torrentcreator/status torrents/export
	torrents/editTracker torrents/filePrio torrents/rename torrents/renameFile
	torrents/renameFolder app/getDirectoryContent app/sendTestEmail rss/setFeedURL app/processInfo)

for tag in "${tags[@]}"; do
	image=lscr.io/linuxserver/qbittorrent:$tag
	docker run -d --rm --name "$name" --network none -e WEBUI_PORT=8080 "$image" >/dev/null
	logged_in=
	for _ in $(seq 1 60); do
		password=$(docker logs "$name" 2>&1 | sed -n 's/.*temporary password is provided for this session: //p' | tail -n1)
		if docker exec "$name" curl -fsS --max-time 2 -c /tmp/cookies \
			-H 'Referer: http://127.0.0.1:8080' --data-urlencode username=admin \
			--data-urlencode "password=${password:-adminadmin}" "$url/auth/login" >/dev/null 2>&1 \
			&& docker exec "$name" grep -q SID /tmp/cookies; then
			logged_in=1
			break
		fi
		sleep 1
	done
	[[ $logged_in ]] || { echo "$tag: login failed" >&2; exit 1; }
	version=$(api "$url/app/version")
	version=${version#v}
	api --data-urlencode 'json={"use_subcategories":true}' \
		"$url/app/setPreferences" >/dev/null

	control=$(status "$url/torrents/doesNotExist")
	[[ $control == 404 ]] || { echo "$version: an action that does not exist answered $control" >&2; exit 1; }
	results='{}'
	for endpoint in "${endpoints[@]}"; do
		results=$(jq --arg key "$endpoint" --argjson code "$(status "$url/$endpoint")" '.[$key] = $code' <<<"$results")
	done

	# A subcategory with no save path, and a colon that toValidPath and
	# toValidFileName name differently.
	api --data-urlencode category=p --data-urlencode savePath=/p "$url/torrents/createCategory" >/dev/null
	api --data-urlencode category=p/x:y "$url/torrents/createCategory" >/dev/null
	docker cp "$scratch/probe.torrent" "$name:/tmp/probe.torrent" >/dev/null
	api -F torrents=@/tmp/probe.torrent -F category=p/x:y -F autoTMM=true \
		-F paused=true -F stopped=true "$url/torrents/add" >/dev/null
	added=
	for _ in $(seq 1 30); do
		api "$url/torrents/info" >"$scratch/info.json"
		if jq -e 'length == 1' "$scratch/info.json" >/dev/null; then
			added=1
			break
		fi
		sleep 1
	done
	[[ $added ]] || { echo "$version: torrent not added" >&2; exit 1; }
	hash=$(jq -r '.[0].hash' "$scratch/info.json")

	# Export first: qui says that 4.6.x exports a torrent without its trackers.
	exported=null
	if [[ $(jq '."torrents/export"' <<<"$results") != 404 ]]; then
		api --data-urlencode "hash=$hash" "$url/torrents/export" >"$scratch/export.torrent"
		exported=$("$scratch/trackers-bin" "$scratch/export.torrent")
	fi

	limits=(--data-urlencode "hashes=$hash" --data-urlencode ratioLimit=-2
		--data-urlencode seedingTimeLimit=-2 --data-urlencode inactiveSeedingTimeLimit=-2)
	without_action=$(status "${limits[@]}" --data-urlencode shareLimitsMode=Default "$url/torrents/setShareLimits")
	without_mode=$(status "${limits[@]}" --data-urlencode shareLimitAction=Default "$url/torrents/setShareLimits")

	api --data-urlencode category=edit --data-urlencode savePath=/edit --data-urlencode downloadPathEnabled=true \
		--data-urlencode downloadPath=/edit-incomplete "$url/torrents/createCategory" >/dev/null
	created=$(api "$url/torrents/categories" | jq '.edit')
	api --data-urlencode category=edit --data-urlencode savePath=/edited "$url/torrents/editCategory" >/dev/null
	edited=$(api "$url/torrents/categories" | jq '.edit')

	webapi=$(api "$url/app/webapiVersion")
	libtorrent=$(api "$url/app/buildInfo" | jq -r .libtorrent)
	digest=$(docker image inspect "$image" --format '{{index .RepoDigests 0}}')
	jq -n --arg version "v$version" --arg webapi "$webapi" --arg libtorrent "$libtorrent" --arg image "$digest" \
		--argjson endpoints "$results" --argjson exported "$exported" \
		--argjson without_action "$without_action" --argjson without_mode "$without_mode" \
		--arg save_path "$(jq -r '.[0].save_path' "$scratch/info.json")" --argjson created "$created" --argjson edited "$edited" \
		'{version: $version, webapi_version: $webapi, libtorrent: $libtorrent, image: $image, network: "none",
		endpoints: $endpoints,
		set_share_limits: {without_share_limit_action: $without_action, without_share_limits_mode: $without_mode},
		subcategory_save_path: $save_path,
		save_path_edit: {before: $created, after: $edited},
		export_trackers: $exported}' >"$scratch/$version.json"
	docker rm -f "$name" >/dev/null
	echo "v$version (Web API $webapi): probed"
done

# Keep the previous captures if any release fails.
for capture in "$scratch"/[0-9]*.json; do
	jq -S . "$capture" >"$(basename "$capture")"
done
