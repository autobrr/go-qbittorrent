package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newCompatibilityClient(handler http.HandlerFunc) *Client {
	return NewClient(Config{Host: "http://qbittorrent.test", APIKey: "fixture"}).WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			return recorder.Result(), nil
		}),
	})
}

func TestAddTorrentSeedModeCompatibility(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fixture.torrent")
	require.NoError(t, os.WriteFile(file, []byte("fixture"), 0o600))
	methods := []struct {
		name string
		add  func(context.Context, *Client, map[string]string) (*TorrentAddResponse, error)
	}{
		{"memory", func(_ context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentFromMemory([]byte("fixture"), opts)
		}},
		{"memory context", func(ctx context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentFromMemoryCtx(ctx, []byte("fixture"), opts)
		}},
		{"batch memory", func(_ context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentsFromMemory([][]byte{[]byte("fixture"), []byte("fixture")}, opts)
		}},
		{"batch memory context", func(ctx context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentsFromMemoryCtx(ctx, [][]byte{[]byte("fixture"), []byte("fixture")}, opts)
		}},
		{"file", func(_ context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentFromFile(file, opts)
		}},
		{"file context", func(ctx context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentFromFileCtx(ctx, file, opts)
		}},
		{"URL", func(_ context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentFromUrl("https://example.invalid/fixture.torrent", opts)
		}},
		{"URL context", func(ctx context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentFromUrlCtx(ctx, "https://example.invalid/fixture.torrent", opts)
		}},
		{"batch URL context", func(ctx context.Context, c *Client, opts map[string]string) (*TorrentAddResponse, error) {
			return c.AddTorrentsFromUrlsCtx(ctx, []string{"https://example.invalid/fixture.torrent", "https://example.invalid/second.torrent"}, opts)
		}},
	}

	for _, method := range methods {
		for _, tc := range []struct {
			name string
			opts map[string]string
			want string
		}{
			{"typed true", (&TorrentAddOptions{SkipHashCheck: true}).Prepare(), "true"},
			{"typed false", (&TorrentAddOptions{SkipHashCheck: false}).Prepare(), ""},
			{"legacy true", map[string]string{"skip_checking": "true"}, "true"},
			{"legacy false", map[string]string{"skip_checking": "false"}, "false"},
			{"new true", map[string]string{"seedMode": "true"}, "true"},
			{"new false", map[string]string{"seedMode": "false"}, "false"},
			{"new true wins", map[string]string{"seedMode": "true", "skip_checking": "false"}, "true"},
			{"new false wins", map[string]string{"seedMode": "false", "skip_checking": "true"}, "false"},
			{"absent", map[string]string{"category": "fixture"}, ""},
			{"nil", nil, ""},
		} {
			t.Run(method.name+"/"+tc.name, func(t *testing.T) {
				calls := 0
				client := newCompatibilityClient(func(w http.ResponseWriter, r *http.Request) {
					calls++
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, "/api/v2/torrents/add", r.URL.Path)
					if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
						require.NoError(t, r.ParseMultipartForm(1<<20))
						defer r.MultipartForm.RemoveAll()
						require.NotEmpty(t, r.MultipartForm.File["torrents"])
					} else {
						require.NoError(t, r.ParseForm())
						require.Contains(t, r.Form.Get("urls"), "https://example.invalid/fixture.torrent")
					}
					// 5.2.2 reads skip_checking. 5.3 reads seedMode.
					for _, key := range []string{"skip_checking", "seedMode"} {
						require.Equal(t, tc.want, r.Form.Get(key))
						require.Equal(t, tc.want != "", r.Form.Has(key))
					}
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte("Ok."))
				})
				before := maps.Clone(tc.opts)
				_, err := method.add(t.Context(), client, tc.opts)
				require.NoError(t, err)
				require.Equal(t, before, tc.opts)
				require.Equal(t, 1, calls)

				if tc.name == "legacy true" {
					delete(tc.opts, "skip_checking")
					tc.want = ""
					_, err = method.add(t.Context(), client, tc.opts)
					require.NoError(t, err)
					require.Empty(t, tc.opts)
					require.Equal(t, 2, calls)
				}
			})
		}
	}
}

func TestRSSRuleSeedModeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		seed string
		want bool
	}{
		{`"skip_checking":true`, true},
		{`"skip_checking":false`, false},
		{`"seed_mode":true`, true},
		{`"seed_mode":false`, false},
		{`"seed_mode":false,"skip_checking":true`, false},
		{`"seed_mode":true,"skip_checking":false`, true},
	} {
		for _, mode := range []string{"Default", "MatchAny", "MatchAll"} {
			t.Run(tc.seed+"/"+mode, func(t *testing.T) {
				want := tc.want
				writes := 0
				client := newCompatibilityClient(func(w http.ResponseWriter, r *http.Request) {
					switch r.Method + " " + r.URL.Path {
					case "GET /api/v2/rss/rules":
						_, _ = fmt.Fprintf(w, `{"fixture":{"priority":1,"torrentParams":{%s,"share_limits_mode":%q,"category":"tv"}}}`, tc.seed, mode)
					case "POST /api/v2/rss/setRule":
						writes++
						require.NoError(t, r.ParseForm())
						require.Equal(t, "fixture", r.Form.Get("ruleName"))
						var wire struct {
							Priority int            `json:"priority"`
							Params   map[string]any `json:"torrentParams"`
						}
						require.NoError(t, json.Unmarshal([]byte(r.Form.Get("ruleDef")), &wire))
						require.Equal(t, 2, wire.Priority)
						require.Equal(t, "tv", wire.Params["category"])
						require.Equal(t, mode, wire.Params["share_limits_mode"])
						require.Equal(t, want, wire.Params["seed_mode"])
						require.Equal(t, want, wire.Params["skip_checking"])
					default:
						t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
					}
				})
				rules, err := client.GetRSSRulesCtx(t.Context())
				require.NoError(t, err)
				rule := rules["fixture"]
				require.NotNil(t, rule.TorrentParams)
				require.Equal(t, tc.want, rule.TorrentParams.SkipChecking)
				rule.Priority = 2
				require.NoError(t, client.SetRSSRuleCtx(t.Context(), "fixture", rule))
				// The existing public field must still control later edits.
				want = !want
				rule.TorrentParams.SkipChecking = want
				require.NoError(t, client.SetRSSRule("fixture", rule))
				require.Equal(t, 2, writes)
			})
		}
	}
}

func TestTorrentCreationDateCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want [3]string
		bad  bool
	}{
		{"legacy", `"timeAdded":"Mon Sep 7 08:00:00 2026","timeStarted":"Mon Sep 7 08:00:01 2026","timeFinished":"Mon Sep 7 08:00:02 2026"`, [3]string{"Mon Sep 7 08:00:00 2026", "Mon Sep 7 08:00:01 2026", "Mon Sep 7 08:00:02 2026"}, false},
		{"numeric", `"timeAdded":0,"timeStarted":1,"timeFinished":2`, [3]string{"1970-01-01T00:00:00Z", "1970-01-01T00:00:01Z", "1970-01-01T00:00:02Z"}, false},
		{"optional dates omitted", `"timeAdded":-1`, [3]string{"", "", ""}, false},
		{"mixed", `"timeAdded":"Mon Sep 7 08:00:00 2026","timeStarted":2147483648`, [3]string{"Mon Sep 7 08:00:00 2026", "2038-01-19T03:14:08Z", ""}, false},
		{"object", `"timeAdded":{}`, [3]string{}, true},
		{"boolean", `"timeStarted":true`, [3]string{}, true},
		{"fraction", `"timeFinished":1.5`, [3]string{}, true},
		{"null", `"timeAdded":null`, [3]string{}, true},
		{"overflow", `"timeAdded":9223372036854775808`, [3]string{}, true},
		{"out of date range", `"timeAdded":9223372036854775807`, [3]string{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newCompatibilityClient(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				switch r.URL.Path {
				case "/api/v2/app/webapiVersion":
					_, _ = w.Write([]byte("2.16.2"))
				case "/api/v2/torrentcreator/status":
					require.Equal(t, "fixture", r.URL.Query().Get("taskID"))
					_, _ = fmt.Fprintf(w, `[{"taskID":"fixture","status":"Finished",%s}]`, tc.body)
				default:
					t.Fatalf("unexpected request: %s", r.URL)
				}
			})
			tasks, err := client.GetTorrentCreationStatusCtx(t.Context(), "fixture")
			if tc.bad {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, tasks, 1)
			require.Equal(t, "fixture", tasks[0].TaskID)
			require.Equal(t, TorrentCreationStatusFinished, tasks[0].Status)
			require.Equal(t, tc.want, [3]string{tasks[0].TimeAdded, tasks[0].TimeStarted, tasks[0].TimeFinished})
		})
	}
}

func TestAppPreferencesCompatibility(t *testing.T) {
	type testCase struct {
		name string
		body string
		want AppPreferences
	}
	cases := []testCase{
		{"legacy", `{"mail_notification_ssl_enabled":true,"export_dir":"/old","export_dir_fin":"/finished"}`, AppPreferences{
			MailNotificationSslEnabled: true, ExportDir: "/old", ExportDirFin: "/finished",
		}},
		{"mixed and false", `{"mail_notification_ssl_enabled":true,"mail_notification_encryption_type":"None","torrent_files_backup_enabled":false,"torrent_files_finished_backup_dir_enabled":false,"remove_torrent_file_backup":false,"queueing_enabled":true}`, AppPreferences{
			MailNotificationSslEnabled: true, MailNotificationEncryptionType: "None", QueueingEnabled: true,
		}},
	}
	for _, encryption := range []string{"None", "STARTTLS", "SMTPS"} {
		cases = append(cases, testCase{encryption, fmt.Sprintf(`{"mail_notification_encryption_type":%q,"torrent_files_backup_enabled":true,"torrent_files_backup_dir":"/backup","torrent_files_finished_backup_dir_enabled":true,"torrent_files_finished_backup_dir":"/finished","remove_torrent_file_backup":true}`, encryption), AppPreferences{
			MailNotificationEncryptionType: encryption,
			TorrentFilesBackupEnabled:      true, TorrentFilesBackupDir: "/backup",
			TorrentFilesFinishedBackupDirEnabled: true, TorrentFilesFinishedBackupDir: "/finished",
			RemoveTorrentFileBackup: true,
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			client := newCompatibilityClient(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /api/v2/app/preferences":
					_, _ = w.Write([]byte(tc.body))
				case "POST /api/v2/app/setPreferences":
					writes++
					require.NoError(t, r.ParseForm())
					require.JSONEq(t, tc.body, r.Form.Get("json"))
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
				}
			})
			prefs, err := client.GetAppPreferencesCtx(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, prefs)
			var write map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.body), &write))
			require.NoError(t, client.SetPreferencesCtx(t.Context(), write))
			require.Equal(t, 1, writes)
		})
	}
}
