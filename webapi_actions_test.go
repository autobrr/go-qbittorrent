package qbittorrent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type webAPIAction struct {
	Method *string   `json:"method"`
	Params *[]string `json:"params"`
}

// loadWebAPICaptures reads the files that the webapi subcommand of
// internal/capture wrote, keyed by qBittorrent version.
func loadWebAPICaptures(t *testing.T) map[string]map[string]webAPIAction {
	t.Helper()
	paths, err := filepath.Glob("testdata/webapi/*.json")
	require.NoError(t, err)
	require.Len(t, paths, 11)
	captures := make(map[string]map[string]webAPIAction, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		var capture struct {
			Actions map[string]webAPIAction `json:"actions"`
		}
		require.NoError(t, json.Unmarshal(data, &capture))
		captures[strings.TrimSuffix(filepath.Base(path), ".json")] = capture.Actions
	}
	return captures
}

func TestWebAPIActionsComplete(t *testing.T) {
	for version, actions := range loadWebAPICaptures(t) {
		t.Run(version, func(t *testing.T) {
			require.NotEmpty(t, actions)
			for name, action := range actions {
				require.NotNil(t, action.Method, name)
				require.Contains(t, []string{"POST", "any"}, *action.Method, name)
				require.NotNil(t, action.Params, name)
			}
		})
	}
}

// TestWebAPIActionsMatchFeatureProbes checks the actions that the source lists
// against the live probes in testdata/features/.
func TestWebAPIActionsMatchFeatureProbes(t *testing.T) {
	for version, actions := range loadWebAPICaptures(t) {
		t.Run(version, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "features", version+".json"))
			require.NoError(t, err)
			var probe struct {
				Endpoints map[string]int `json:"endpoints"`
			}
			require.NoError(t, json.Unmarshal(data, &probe))
			require.NotEmpty(t, probe.Endpoints)
			for name, code := range probe.Endpoints {
				_, listed := actions[name]
				require.Equal(t, code != 404, listed, "%s answered %d", name, code)
			}
		})
	}
}
