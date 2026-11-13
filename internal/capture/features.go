package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autobrr/go-torrent/metainfo"
)

// featureVersions are the newest release of each stable Web API version from 4.3.9 up.
var featureVersions = []string{"4.3.9", "4.4.0", "4.4.5", "4.5.0", "4.5.5", "4.6.0", "4.6.7", "5.0.4", "5.1.4", "5.2.4", "5.3.0"}

// POST-only actions answer GET with 405. Releases before 4.5 run the action on a
// GET and answer 400 for the missing parameters, so nothing changes. An action
// that does not exist answers 404.
var probedActions = []string{"torrents/setTags", "torrents/setComment", "torrentcreator/status", "torrents/export",
	"torrents/editTracker", "torrents/filePrio", "torrents/rename", "torrents/renameFile",
	"torrents/renameFolder", "app/getDirectoryContent", "app/sendTestEmail", "rss/setFeedURL", "app/processInfo"}

// captureFeatures probes the features that qui gates by Web API version, and
// writes one file per release to testdata/features/. qBittorrent runs with no network.
func captureFeatures(scratch string) error {
	probe := filepath.Join(scratch, "probe.bin")
	if err := mkbrr(probe, 65536, "--tracker", "http://127.0.0.1:1/announce"); err != nil {
		return err
	}
	// Keep the previous captures if any release fails.
	captures := map[string][]byte{}
	for _, v := range featureVersions {
		version, capture, err := probeFeatures(image(v), filepath.Join(scratch, "probe.torrent"))
		removeContainer()
		if err != nil {
			return err
		}
		captures[version+".json"] = capture
	}
	for name, data := range captures {
		if err := writeSorted("features", name, data); err != nil {
			return err
		}
	}
	return nil
}

func probeFeatures(image, torrent string) (string, []byte, error) {
	c, err := startContainer(image, 8080, "--network", "none")
	if err != nil {
		return "", nil, err
	}
	out, err := c.api("app/version")
	if err != nil {
		return "", nil, err
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	if _, err := c.api("app/setPreferences", "--data-urlencode", `json={"use_subcategories":true}`); err != nil {
		return "", nil, err
	}

	code, err := c.status("torrents/doesNotExist")
	if err != nil {
		return "", nil, err
	}
	if code != 404 {
		return "", nil, fmt.Errorf("%s: an action that does not exist answered %d", version, code)
	}
	endpoints := map[string]int{}
	for _, a := range probedActions {
		if endpoints[a], err = c.status(a); err != nil {
			return "", nil, err
		}
	}

	// A subcategory with no save path, and a colon that toValidPath and
	// toValidFileName name differently.
	if _, err := c.api("torrents/createCategory", "--data-urlencode", "category=p", "--data-urlencode", "savePath=/p"); err != nil {
		return "", nil, err
	}
	if _, err := c.api("torrents/createCategory", "--data-urlencode", "category=p/x:y"); err != nil {
		return "", nil, err
	}
	if _, err := docker("cp", torrent, containerName+":/tmp/probe.torrent"); err != nil {
		return "", nil, err
	}
	if _, err := c.api("torrents/add", "-F", "torrents=@/tmp/probe.torrent", "-F", "category=p/x:y",
		"-F", "autoTMM=true", "-F", "paused=true", "-F", "stopped=true"); err != nil {
		return "", nil, err
	}
	var info []struct {
		Hash     string `json:"hash"`
		SavePath string `json:"save_path"`
	}
	added, err := poll(30, func() (bool, error) {
		err := c.getJSON(&info, "torrents/info")
		return len(info) == 1, err
	})
	if err != nil {
		return "", nil, err
	}
	if !added {
		return "", nil, fmt.Errorf("%s: torrent not added", version)
	}
	hash := info[0].Hash

	// Export first: qui says that 4.6.x exports a torrent without its trackers.
	var exported any
	if endpoints["torrents/export"] != 404 {
		out, err := c.api("torrents/export", "--data-urlencode", "hash="+hash)
		if err != nil {
			return "", nil, err
		}
		mi, err := metainfo.Load(bytes.NewReader(out))
		if err != nil {
			return "", nil, err
		}
		exported = map[string]any{"announce": mi.Announce, "announce_list": mi.AnnounceList}
	}

	limits := []string{"--data-urlencode", "hashes=" + hash, "--data-urlencode", "ratioLimit=-2",
		"--data-urlencode", "seedingTimeLimit=-2", "--data-urlencode", "inactiveSeedingTimeLimit=-2"}
	withoutAction, err := c.status("torrents/setShareLimits", append(limits, "--data-urlencode", "shareLimitsMode=Default")...)
	if err != nil {
		return "", nil, err
	}
	withoutMode, err := c.status("torrents/setShareLimits", append(limits, "--data-urlencode", "shareLimitAction=Default")...)
	if err != nil {
		return "", nil, err
	}

	if _, err := c.api("torrents/createCategory", "--data-urlencode", "category=edit", "--data-urlencode", "savePath=/edit",
		"--data-urlencode", "downloadPathEnabled=true", "--data-urlencode", "downloadPath=/edit-incomplete"); err != nil {
		return "", nil, err
	}
	var created, edited map[string]json.RawMessage
	if err := c.getJSON(&created, "torrents/categories"); err != nil {
		return "", nil, err
	}
	if _, err := c.api("torrents/editCategory", "--data-urlencode", "category=edit", "--data-urlencode", "savePath=/edited"); err != nil {
		return "", nil, err
	}
	if err := c.getJSON(&edited, "torrents/categories"); err != nil {
		return "", nil, err
	}

	webapi, err := c.api("app/webapiVersion")
	if err != nil {
		return "", nil, err
	}
	var build struct {
		Libtorrent string `json:"libtorrent"`
	}
	if err := c.getJSON(&build, "app/buildInfo"); err != nil {
		return "", nil, err
	}
	digest, err := c.digest()
	if err != nil {
		return "", nil, err
	}
	capture, err := json.Marshal(map[string]any{
		"version": "v" + version, "webapi_version": strings.TrimSpace(string(webapi)), "libtorrent": build.Libtorrent,
		"image": digest, "network": "none", "endpoints": endpoints,
		"set_share_limits":      map[string]int{"without_share_limit_action": withoutAction, "without_share_limits_mode": withoutMode},
		"subcategory_save_path": info[0].SavePath,
		"save_path_edit":        map[string]json.RawMessage{"before": created["edit"], "after": edited["edit"]},
		"export_trackers":       exported,
	})
	fmt.Printf("v%s (Web API %s): probed\n", version, bytes.TrimSpace(webapi))
	return version, capture, err
}
