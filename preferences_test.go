package qbittorrent

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadPreferenceCaptures reads the app/preferences responses that
// testdata/preferences/capture.sh wrote, keyed by qBittorrent version.
func loadPreferenceCaptures(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob("testdata/preferences/*.json")
	require.NoError(t, err)
	require.Len(t, paths, 8)
	captures := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		captures[strings.TrimSuffix(filepath.Base(path), ".json")] = data
	}
	return captures
}

func sentKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	return slices.Sorted(maps.Keys(fields))
}

// fieldKeys returns the json keys of the AppPreferences fields.
func fieldKeys() map[string]bool {
	keys := map[string]bool{}
	typ := reflect.TypeFor[AppPreferences]()
	for i := range typ.NumField() {
		if key, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ","); key != "" {
			keys[key] = true
		}
	}
	return keys
}

func TestAppPreferencesCapturesHaveFields(t *testing.T) {
	known := fieldKeys()
	for version, data := range loadPreferenceCaptures(t) {
		t.Run(version, func(t *testing.T) {
			var unknown []string
			for _, key := range sentKeys(t, data) {
				if !known[key] {
					unknown = append(unknown, key)
				}
			}
			require.Empty(t, unknown, "AppPreferences has no field for these keys")

			var prefs AppPreferences
			require.NoError(t, json.Unmarshal(data, &prefs))
		})
	}
}

func TestAppPreferencesHasMatchesCapture(t *testing.T) {
	for version, data := range loadPreferenceCaptures(t) {
		t.Run(version, func(t *testing.T) {
			var prefs AppPreferences
			require.NoError(t, json.Unmarshal(data, &prefs))
			sent := sentKeys(t, data)
			for _, key := range sent {
				require.True(t, prefs.Has(key), key)
			}
			for key := range fieldKeys() {
				if !slices.Contains(sent, key) {
					require.False(t, prefs.Has(key), key)
				}
			}
		})
	}
}

func TestAppPreferencesMarshalWritesSentKeys(t *testing.T) {
	for version, data := range loadPreferenceCaptures(t) {
		t.Run(version, func(t *testing.T) {
			var prefs AppPreferences
			require.NoError(t, json.Unmarshal(data, &prefs))
			encoded, err := json.Marshal(prefs)
			require.NoError(t, err)

			var want, got map[string]any
			require.NoError(t, json.Unmarshal(data, &want))
			delete(want, "web_ui_api_key")
			require.NoError(t, json.Unmarshal(encoded, &got))
			require.Equal(t, want, got)

			// A pointer encodes the same way.
			encoded, err = json.Marshal(&prefs)
			require.NoError(t, err)
			got = nil
			require.NoError(t, json.Unmarshal(encoded, &got))
			require.Equal(t, want, got)
		})
	}
}

func TestAppPreferencesMarshalDropsAPIKey(t *testing.T) {
	var prefs AppPreferences
	require.NoError(t, json.Unmarshal([]byte(`{"web_ui_api_key":"qbt_secret","queueing_enabled":true}`), &prefs))
	require.True(t, prefs.Has("web_ui_api_key"))
	require.Equal(t, "qbt_secret", prefs.WebUIAPIKey)

	encoded, err := json.Marshal(prefs)
	require.NoError(t, err)
	require.JSONEq(t, `{"queueing_enabled":true}`, string(encoded))

	// A value that was never decoded has no record of sent keys, so every other field is written.
	encoded, err = json.Marshal(AppPreferences{WebUIAPIKey: "qbt_secret"})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"queueing_enabled":false`)
	require.NotContains(t, string(encoded), "web_ui_api_key")
	require.NotContains(t, string(encoded), "qbt_secret")
}

func TestAppPreferencesAddStopped(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"4.x name", `{"start_paused_enabled":true}`, true},
		{"5.x name", `{"add_stopped_enabled":true}`, true},
		{"5.x name off", `{"add_stopped_enabled":false}`, false},
		{"5.3 session start_paused is ignored", `{"add_stopped_enabled":false,"start_paused":true}`, false},
		{"only session start_paused", `{"start_paused":true}`, false},
		{"none", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prefs AppPreferences
			require.NoError(t, json.Unmarshal([]byte(tc.body), &prefs))
			require.Equal(t, tc.want, prefs.AddStopped())
		})
	}

	captures := loadPreferenceCaptures(t)
	for version, data := range captures {
		var prefs AppPreferences
		require.NoError(t, json.Unmarshal(data, &prefs))
		require.False(t, prefs.AddStopped(), version)

		// Turn on the key that this version sends.
		var fields map[string]any
		require.NoError(t, json.Unmarshal(data, &fields))
		key := "add_stopped_enabled"
		if _, ok := fields[key]; !ok {
			key = "start_paused_enabled"
		}
		fields[key] = true
		data, err := json.Marshal(fields)
		require.NoError(t, err)
		prefs = AppPreferences{}
		require.NoError(t, json.Unmarshal(data, &prefs))
		require.True(t, prefs.AddStopped(), version)
	}
}

func TestAppPreferencesCopyKeepsOwnRecord(t *testing.T) {
	var original AppPreferences
	require.NoError(t, json.Unmarshal([]byte(`{"queueing_enabled":true}`), &original))

	clone := original
	require.NoError(t, json.Unmarshal([]byte(`{"dht":true}`), &clone))

	require.True(t, clone.Has("queueing_enabled"))
	require.True(t, clone.Has("dht"))
	require.True(t, original.Has("queueing_enabled"))
	require.False(t, original.Has("dht"))

	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	require.JSONEq(t, `{"queueing_enabled":true}`, string(encoded))
}
