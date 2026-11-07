package main

import (
	"encoding/json"
	"fmt"
	"slices"
)

// preferenceVersions are the newest release of each qBittorrent minor version from 4.3 up.
var preferenceVersions = []string{"4.3.9", "4.4.5", "4.5.5", "4.6.7", "5.0.4", "5.1.4", "5.2.4", "5.3.0"}

// capturePreferences writes one app/preferences response from a container with
// no torrents to testdata/preferences/<version>.json, for each version, or for
// every version in preferenceVersions when versions is empty.
func capturePreferences(versions []string) error {
	if len(versions) == 0 {
		versions = preferenceVersions
	}
	for _, version := range versions {
		if !slices.Contains(preferenceVersions, version) {
			return fmt.Errorf("%s: not one of %v", version, preferenceVersions)
		}
		err := capturePreference(version)
		removeContainer()
		if err != nil {
			return err
		}
	}
	return nil
}

func capturePreference(version string) error {
	c, err := startContainer(image(version), 18080,
		"-e", "QBT_WEBUI_PORT=18080", "-e", "QBT_LEGAL_NOTICE=confirm")
	if err != nil {
		return err
	}
	out, err := c.api("app/preferences")
	if err != nil {
		return err
	}
	var prefs map[string]json.RawMessage
	if err := json.Unmarshal(out, &prefs); err != nil {
		return err
	}
	if err := writeSorted("preferences", version+".json", out); err != nil {
		return err
	}
	fmt.Printf("%s: %d keys\n", version, len(prefs))
	return nil
}
