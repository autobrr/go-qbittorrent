package qbittorrent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFeatureProbesComplete checks that each file that `go run . features` in
// internal/capture wrote has a result for each probe. qui tests its version table against these files.
func TestFeatureProbesComplete(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "features", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			body, err := os.ReadFile(file)
			require.NoError(t, err)
			var probe struct {
				Version             *string         `json:"version"`
				WebAPIVersion       *string         `json:"webapi_version"`
				Libtorrent          *string         `json:"libtorrent"`
				Endpoints           map[string]*int `json:"endpoints"`
				SetShareLimits      map[string]*int `json:"set_share_limits"`
				SubcategorySavePath *string         `json:"subcategory_save_path"`
				SavePathEdit        struct {
					Before *struct{} `json:"before"`
					After  *struct{} `json:"after"`
				} `json:"save_path_edit"`
				// null when the release has no export endpoint.
				ExportTrackers json.RawMessage `json:"export_trackers"`
			}
			require.NoError(t, json.Unmarshal(body, &probe))

			require.NotNil(t, probe.Version)
			require.Equal(t, "v"+strings.TrimSuffix(filepath.Base(file), ".json"), *probe.Version)
			require.NotNil(t, probe.WebAPIVersion)
			require.NotEmpty(t, *probe.WebAPIVersion)
			require.NotNil(t, probe.Libtorrent)
			for _, endpoint := range []string{"torrents/setTags", "torrents/setComment", "torrentcreator/status",
				"torrents/export", "torrents/editTracker", "torrents/filePrio", "torrents/rename",
				"torrents/renameFile", "torrents/renameFolder", "app/getDirectoryContent",
				"app/sendTestEmail", "rss/setFeedURL", "app/processInfo"} {
				require.NotNil(t, probe.Endpoints[endpoint], endpoint)
			}
			require.NotNil(t, probe.SetShareLimits["without_share_limit_action"])
			require.NotNil(t, probe.SetShareLimits["without_share_limits_mode"])
			require.NotNil(t, probe.SubcategorySavePath)
			require.NotNil(t, probe.SavePathEdit.Before)
			require.NotNil(t, probe.SavePathEdit.After)
			require.NotNil(t, probe.ExportTrackers)
			require.Equal(t, *probe.Endpoints["torrents/export"] == 404, string(probe.ExportTrackers) == "null",
				"export_trackers is null only when the export endpoint is missing")
		})
	}
}
