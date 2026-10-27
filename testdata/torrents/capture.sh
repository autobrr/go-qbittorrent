#!/usr/bin/env bash
# Capture real torrent responses. Tests only read the saved JSON files.
# Requires Docker, curl in the images, jq, and mkbrr. No network is used by qBittorrent.
# Usage: testdata/torrents/capture.sh
set -euo pipefail

cd "$(dirname "$0")"
scratch=$(mktemp -d)
name=qbt-torrent-capture-${scratch##*/}
cleanup() {
	docker rm -f "$name" >/dev/null 2>&1 || true
	rm -rf "$scratch"
}
trap cleanup EXIT

# Two 64 KiB pieces keep hashing small and make the recheck observable.
truncate -s 131072 "$scratch/pieces.bin"
nice -n 19 env GOMAXPROCS=2 mkbrr create "$scratch/pieces.bin" --workers 1 \
	--piece-length 16 --private --no-creator --output "$scratch/pieces.torrent" --quiet

api() {
	docker exec "$name" curl -fsS --max-time 5 -b /tmp/cookies \
		-H 'Referer: http://127.0.0.1:8080' "$@"
}
url=http://127.0.0.1:8080/api/v2
fields='["pieces_num", "pieces_have", "piece_size", "total_wasted", "creation_date", "connections_count", "connections_limit"]'

for version in 5.1.4 5.2.4; do
	image=lscr.io/linuxserver/qbittorrent:$version
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
	[[ $logged_in ]] || { echo "$version: login failed" >&2; exit 1; }
	actual=$(api "$url/app/version")
	[[ $actual == "v$version" ]] || { echo "Expected v$version, got $actual" >&2; exit 1; }
	api --data-urlencode 'json={"dht":false,"pex":false,"lsd":false,"random_port":false,"upnp":false}' "$url/app/setPreferences" >/dev/null
	docker exec "$name" mkdir -p /downloads
	docker exec "$name" chown abc:abc /downloads
	docker cp "$scratch/pieces.torrent" "$name:/tmp/pieces.torrent" >/dev/null
	api -F torrents=@/tmp/pieces.torrent -F savepath=/downloads -F autoTMM=false \
		-F paused=true -F stopped=true "$url/torrents/add" >/dev/null
	ready=
	for _ in $(seq 1 30); do
		api "$url/sync/maindata?rid=0" >"$scratch/$version-full.json"
		if jq -e '.full_update == true and (.torrents | length) == 1 and ([.torrents[].state] | all(. == "stoppedDL" or . == "pausedDL"))' \
			"$scratch/$version-full.json" >/dev/null; then
			ready=1
			break
		fi
		sleep 1
	done
	[[ $ready ]] || { echo "$version: torrent not ready" >&2; exit 1; }
	api "$url/torrents/info" >"$scratch/$version-info.json"
	hash=$(jq -r '.torrents | keys[0]' "$scratch/$version-full.json")
	rid=$(jq -r .rid "$scratch/$version-full.json")
	if [[ $version == 5.2.4 ]]; then
		jq -e --argjson fields "$fields" '.torrents[] | . as $t | ($fields | all(. as $key | $t | has($key))) and .pieces_num == 2 and .pieces_have == 0 and .piece_size == 65536' \
			"$scratch/$version-full.json" >/dev/null
		jq -e --argjson fields "$fields" 'length == 1 and (.[0] | . as $t | $fields | all(. as $key | $t | has($key)))' \
			"$scratch/$version-info.json" >/dev/null
	else
		jq -e --argjson fields "$fields" '.torrents[] | . as $t | $fields | all(. as $key | $t | has($key) | not)' \
			"$scratch/$version-full.json" >/dev/null
		jq -e --argjson fields "$fields" 'length == 1 and (.[0] | . as $t | $fields | all(. as $key | $t | has($key) | not))' \
			"$scratch/$version-info.json" >/dev/null
	fi
	# Local rechecking changes pieces_have without peers, trackers, or a download.
	docker cp "$scratch/pieces.bin" "$name:/downloads/pieces.bin" >/dev/null
	api --data-urlencode "hashes=$hash" "$url/torrents/recheck" >/dev/null
	complete=
	for _ in $(seq 1 30); do
		api "$url/sync/maindata?rid=$rid" >"$scratch/$version-delta.json"
		if jq -e --arg hash "$hash" '.full_update != true and .torrents[$hash].completed == 131072' \
			"$scratch/$version-delta.json" >/dev/null; then
			complete=1
			break
		fi
		sleep 1
	done
	[[ $complete ]] || { echo "$version: recheck did not produce a delta" >&2; exit 1; }
	if [[ $version == 5.2.4 ]]; then
		jq -e --arg hash "$hash" '.torrents[$hash].pieces_have == 2' "$scratch/$version-delta.json" >/dev/null
	fi
	digest=$(docker image inspect "$image" --format '{{index .RepoDigests 0}}')
	jq -n --arg version "$actual" --arg image "$digest" --arg hash "$hash" \
		'{version: $version, image: $image, hash: $hash, network: "none", content_bytes: 131072, piece_bytes: 65536}' \
		>"$scratch/$version-source.json"
	docker rm -f "$name" >/dev/null
	echo "$actual: full, info, and recheck delta captured"
done

# Keep the previous captures if either version fails its assertions.
for capture in "$scratch"/*.json; do
	jq -S . "$capture" >"$(basename "$capture")"
done
