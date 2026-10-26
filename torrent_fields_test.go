package qbittorrent

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTorrent52RecordedFields(t *testing.T) {
	const hash = "909a049b1e5761e6a47bf0638ca05ee88e27734a"
	fields := func(torrent Torrent) []int64 {
		return []int64{torrent.PiecesNum, torrent.PiecesHave, torrent.PieceSize, torrent.TotalWasted,
			torrent.CreationDate, torrent.ConnectionsCount, torrent.ConnectionsLimit}
	}
	for _, tc := range []struct {
		version string
		full    []int64
		delta   []int64
	}{
		{"5.1.4", []int64{0, 0, 0, 0, 0, 0, 0}, []int64{0, 0, 0, 0, 0, 0, 0}},
		{"5.2.4", []int64{2, 0, 65536, 0, 1793047865, 0, 100}, []int64{2, 2, 65536, 0, 1793047865, 0, 100}},
	} {
		t.Run(tc.version, func(t *testing.T) {
			read := func(kind string) []byte {
				body, err := os.ReadFile(filepath.Join("testdata", "torrents", tc.version+"-"+kind+".json"))
				require.NoError(t, err)
				return body
			}
			full, info, delta := read("full"), read("info"), read("delta")
			var initial MainData
			require.NoError(t, json.Unmarshal(full, &initial))
			client := newCompatibilityClient(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v2/torrents/info":
					_, _ = w.Write(info)
				case "/api/v2/sync/maindata":
					switch r.URL.Query().Get("rid") {
					case "0":
						_, _ = w.Write(full)
					case strconv.FormatInt(initial.Rid, 10):
						_, _ = w.Write(delta)
					default:
						t.Fatalf("unexpected response ID: %s", r.URL.RawQuery)
					}
				default:
					t.Fatalf("unexpected endpoint: %s", r.URL.Path)
				}
			})

			torrents, err := client.GetTorrentsCtx(t.Context(), TorrentFilterOptions{})
			require.NoError(t, err)
			require.Len(t, torrents, 1)
			require.Equal(t, tc.full, fields(torrents[0]))

			data := &MainData{}
			require.NoError(t, data.Update(t.Context(), client))
			require.True(t, data.FullUpdate)
			require.Contains(t, data.Torrents, hash)
			require.Equal(t, tc.full, fields(data.Torrents[hash]))

			require.NoError(t, data.Update(t.Context(), client))
			require.False(t, data.FullUpdate)
			require.Equal(t, tc.delta, fields(data.Torrents[hash]))
			require.Equal(t, "pieces.bin", data.Torrents[hash].Name)
		})
	}
}

func TestMainDataTorrent52PartialFields(t *testing.T) {
	data := &MainData{Torrents: map[string]Torrent{"hash": {Name: "keep"}}}
	for _, want := range []Torrent{
		{Name: "keep", PiecesNum: 4, PiecesHave: 3, PieceSize: 131072, TotalWasted: 123,
			CreationDate: 1700000000, ConnectionsCount: 2, ConnectionsLimit: -1},
		{Name: "keep"},
	} {
		data.UpdateWithRawData(map[string]any{
			"torrents": map[string]any{"hash": map[string]any{
				"pieces_num": float64(want.PiecesNum), "pieces_have": float64(want.PiecesHave),
				"piece_size": float64(want.PieceSize), "total_wasted": float64(want.TotalWasted),
				"creation_date": float64(want.CreationDate), "connections_count": float64(want.ConnectionsCount),
				"connections_limit": float64(want.ConnectionsLimit),
			}},
		}, &MainData{})
		require.Equal(t, want, data.Torrents["hash"])
	}
}
