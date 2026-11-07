package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/version"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

var torrentVersions = []string{"4.6.7", "5.0.0", "5.1.4", "5.2.4", "5.3.0"}

// pieceFields are the torrent fields that qBittorrent 5.2.0 added.
var pieceFields = []string{"pieces_num", "pieces_have", "piece_size", "total_wasted", "creation_date", "connections_count", "connections_limit"}

type maindata struct {
	FullUpdate bool               `json:"full_update"`
	Rid        int                `json:"rid"`
	Torrents   map[string]torrent `json:"torrents"`
}

type torrent map[string]json.RawMessage

func (t torrent) is(key, value string) bool { return string(t[key]) == value }

func (t torrent) has(want bool) bool {
	for _, key := range pieceFields {
		if _, ok := t[key]; ok != want {
			return false
		}
	}
	return true
}

// captureTorrents writes the sync/maindata, torrents/info, and recheck delta
// responses of one stopped torrent to testdata/torrents/. qBittorrent runs with no network.
func captureTorrents(scratch string) error {
	// Two 64 KiB pieces keep hashing small and make the recheck observable.
	content := filepath.Join(scratch, "pieces.bin")
	if err := mkbrr(content, 131072, "--private"); err != nil {
		return err
	}
	// Keep the previous captures if any version fails its assertions.
	captures := map[string][]byte{}
	for _, v := range torrentVersions {
		err := captureTorrent(v, scratch, captures)
		removeContainer()
		if err != nil {
			return err
		}
	}
	for name, data := range captures {
		if err := writeSorted("torrents", name, data); err != nil {
			return err
		}
	}
	return nil
}

func captureTorrent(v, scratch string, captures map[string][]byte) error {
	c, err := startContainer(image(v), 8080, "--network", "none")
	if err != nil {
		return err
	}
	actual, err := c.api("app/version")
	if err != nil {
		return err
	}
	actual = bytes.TrimSpace(actual)
	if string(actual) != "v"+v {
		return fmt.Errorf("expected v%s, got %s", v, actual)
	}
	if _, err := c.api("app/setPreferences", "--data-urlencode",
		`json={"dht":false,"pex":false,"lsd":false,"random_port":false,"upnp":false}`); err != nil {
		return err
	}
	if _, err := docker("exec", containerName, "mkdir", "-p", "/downloads"); err != nil {
		return err
	}
	if _, err := docker("exec", containerName, "chown", "abc:abc", "/downloads"); err != nil {
		return err
	}
	if _, err := docker("cp", filepath.Join(scratch, "pieces.torrent"), containerName+":/tmp/pieces.torrent"); err != nil {
		return err
	}
	if _, err := c.api("torrents/add", "-F", "torrents=@/tmp/pieces.torrent", "-F", "savepath=/downloads",
		"-F", "autoTMM=false", "-F", "paused=true", "-F", "stopped=true"); err != nil {
		return err
	}

	var full maindata
	ready, err := poll(30, func() (bool, error) {
		out, err := c.api("sync/maindata?rid=0")
		if err != nil {
			return false, err
		}
		captures[v+"-full.json"] = out
		full = maindata{}
		if err := json.Unmarshal(out, &full); err != nil {
			return false, err
		}
		if !full.FullUpdate || len(full.Torrents) != 1 {
			return false, nil
		}
		for _, t := range full.Torrents {
			if !t.is("state", `"stoppedDL"`) && !t.is("state", `"pausedDL"`) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%s: torrent not ready", v)
	}
	hash := slices.Collect(maps.Keys(full.Torrents))[0]

	out, err := c.api("torrents/info")
	if err != nil {
		return err
	}
	captures[v+"-info.json"] = out
	var info []torrent
	if err := json.Unmarshal(out, &info); err != nil {
		return err
	}
	if captures[v+"-info-trackers.json"], err = c.api("torrents/info?includeTrackers=true"); err != nil {
		return err
	}
	newFields := version.Compare("go"+v, "go5.2.0") >= 0
	t := full.Torrents[hash]
	if !t.has(newFields) || len(info) != 1 || !info[0].has(newFields) ||
		newFields && !(t.is("pieces_num", "2") && t.is("pieces_have", "0") && t.is("piece_size", "65536")) {
		return fmt.Errorf("%s: the piece fields of the torrent are not as expected", v)
	}

	// Local rechecking changes pieces_have without peers, trackers, or a download.
	if _, err := docker("cp", filepath.Join(scratch, "pieces.bin"), containerName+":/downloads/pieces.bin"); err != nil {
		return err
	}
	if _, err := c.api("torrents/recheck", "--data-urlencode", "hashes="+hash); err != nil {
		return err
	}
	var delta maindata
	complete, err := poll(30, func() (bool, error) {
		out, err := c.api(fmt.Sprintf("sync/maindata?rid=%d", full.Rid))
		if err != nil {
			return false, err
		}
		captures[v+"-delta.json"] = out
		delta = maindata{}
		if err := json.Unmarshal(out, &delta); err != nil {
			return false, err
		}
		return !delta.FullUpdate && delta.Torrents[hash].is("completed", "131072"), nil
	})
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("%s: recheck did not produce a delta", v)
	}
	if newFields && !delta.Torrents[hash].is("pieces_have", "2") {
		return fmt.Errorf("%s: the delta has no pieces_have of 2", v)
	}

	webapi, err := c.api("app/webapiVersion")
	if err != nil {
		return err
	}
	digest, err := c.digest()
	if err != nil {
		return err
	}
	captures[v+"-source.json"], err = json.Marshal(map[string]any{
		"version": string(actual), "webapi_version": strings.TrimSpace(string(webapi)), "image": digest, "hash": hash,
		"network": "none", "content_bytes": 131072, "piece_bytes": 65536,
	})
	fmt.Printf("%s: full, info, and recheck delta captured\n", actual)
	return err
}
