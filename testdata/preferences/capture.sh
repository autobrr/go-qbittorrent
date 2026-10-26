#!/usr/bin/env bash
# Capture one app/preferences response for each qBittorrent minor version.
#
# Each capture comes from a local throwaway container with no torrents.
# The tests in preferences_test.go only read the files that this script writes.
#
# Usage: testdata/preferences/capture.sh [version...]
# With no arguments, the script captures every version in the list below.
set -euo pipefail

cd "$(dirname "$0")"

port=18080
name=qbt-preferences-capture

# version=image
declare -A images=(
	[4.3.9]=lscr.io/linuxserver/qbittorrent:14.3.9
	[4.4.5]=lscr.io/linuxserver/qbittorrent:4.4.5
	[4.5.5]=lscr.io/linuxserver/qbittorrent:4.5.5
	[4.6.7]=lscr.io/linuxserver/qbittorrent:4.6.7
	[5.0.4]=lscr.io/linuxserver/qbittorrent:5.0.4
	[5.1.4]=lscr.io/linuxserver/qbittorrent:5.1.4
	[5.2.4]=lscr.io/linuxserver/qbittorrent:5.2.4
	# No image of 5.3.0rc1 exists. This alpha build of master (2026-10-11) sends the
	# keys of release-5.3.0rc1 and two more keys, which the script removes below.
	# Replace it with the linuxserver image when 5.3.0 is released.
	[5.3.0rc1]=qbittorrentofficial/qbittorrent-nox@sha256:8353cba072aeee388a42e89d4f298bc55006ad33f48228dfeb8d54d9f97dc9c2
)

# Keys that the image sends but the release does not.
declare -A extra_keys=(
	[5.3.0rc1]='confirm_remove_all_tags confirm_remove_tracker_from_all_torrents'
)

cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

capture() {
	local version=$1 image=${images[$1]}
	cleanup
	docker run -d --rm --name "$name" \
		-e WEBUI_PORT=$port -e QBT_WEBUI_PORT=$port -e QBT_LEGAL_NOTICE=confirm \
		-p 127.0.0.1:$port:$port "$image" >/dev/null

	local url=http://127.0.0.1:$port jar out
	jar=$(mktemp)
	out=$(mktemp)
	local logged_in=
	for _ in $(seq 1 60); do
		# qBittorrent 4.6.1 and later print a temporary password. Older versions use adminadmin.
		local password
		password=$(docker logs "$name" 2>&1 | sed -n 's/.*temporary password is provided for this session: //p' | tail -n1)
		# qBittorrent 5.3 answers a login with 204 and no body, so look for the session cookie.
		if curl -sf -o /dev/null -c "$jar" -H "Referer: $url" --data-urlencode username=admin \
			--data-urlencode "password=${password:-adminadmin}" "$url/api/v2/auth/login" && grep -q SID "$jar"; then
			logged_in=1
			break
		fi
		sleep 1
	done
	if [[ -z $logged_in ]]; then
		rm -f "$jar" "$out"
		echo "$version: could not log in to $image" >&2
		return 1
	fi

	local drop='.'
	for key in ${extra_keys[$version]:-}; do drop+=" | del(.$key)"; done
	# Write to a temporary file first, so that a failed request keeps the old capture.
	curl -sf -b "$jar" "$url/api/v2/app/preferences" | jq -S "$drop" >"$out"
	mv "$out" "$version.json"
	rm -f "$jar"
	echo "$version: $(jq length "$version.json") keys"
	cleanup
}

if (($# == 0)); then
	set -- $(printf '%s\n' "${!images[@]}" | sort -V)
fi
for version in "$@"; do
	capture "$version"
done
