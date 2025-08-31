package qbittorrent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTorrentAddOptions_Prepare(t *testing.T) {
	type fields struct {
		Stopped            bool
		Paused             bool
		SkipHashCheck      bool
		ContentLayout      ContentLayout
		SavePath           string
		DownloadPath       string
		UseDownloadPath    bool
		AutoTMM            bool
		Category           string
		Tags               string
		LimitUploadSpeed   int64
		LimitDownloadSpeed int64
		LimitRatio         float64
		LimitSeedTime      int64
		Rename             string
		FirstLastPiecePrio bool
		SequentialDownload bool
	}
	tests := []struct {
		name   string
		fields fields
		want   map[string]string
	}{
		{
			name: "test_01",
			fields: fields{
				Paused:             false,
				SkipHashCheck:      true,
				ContentLayout:      "",
				SavePath:           "/home/test/torrents",
				AutoTMM:            false,
				Category:           "test",
				Tags:               "limited,slow",
				LimitUploadSpeed:   100000,
				LimitDownloadSpeed: 100000,
				LimitRatio:         2.0,
				LimitSeedTime:      100,
			},
			want: map[string]string{
				"paused":             "false",
				"stopped":            "false",
				"skip_checking":      "true",
				"autoTMM":            "false",
				"firstLastPiecePrio": "false",
				"ratioLimit":         "2.00",
				"savepath":           "/home/test/torrents",
				"seedingTimeLimit":   "100",
				"category":           "test",
				"tags":               "limited,slow",
				"upLimit":            "102400000",
				"dlLimit":            "102400000",
			},
		},
		{
			name: "test_02",
			fields: fields{
				Paused:             false,
				SkipHashCheck:      true,
				ContentLayout:      ContentLayoutSubfolderCreate,
				SavePath:           "/home/test/torrents",
				AutoTMM:            false,
				Category:           "test",
				Tags:               "limited,slow",
				LimitUploadSpeed:   100000,
				LimitDownloadSpeed: 100000,
				LimitRatio:         2.0,
				LimitSeedTime:      100,
			},
			want: map[string]string{
				"paused":             "false",
				"stopped":            "false",
				"skip_checking":      "true",
				"root_folder":        "true",
				"contentLayout":      "Subfolder",
				"autoTMM":            "false",
				"firstLastPiecePrio": "false",
				"ratioLimit":         "2.00",
				"savepath":           "/home/test/torrents",
				"seedingTimeLimit":   "100",
				"category":           "test",
				"tags":               "limited,slow",
				"upLimit":            "102400000",
				"dlLimit":            "102400000",
			},
		},
		{
			name: "test_03",
			fields: fields{
				Paused:             false,
				SkipHashCheck:      true,
				ContentLayout:      ContentLayoutSubfolderNone,
				SavePath:           "/home/test/torrents",
				AutoTMM:            false,
				Category:           "test",
				Tags:               "limited,slow",
				LimitUploadSpeed:   100000,
				LimitDownloadSpeed: 100000,
				LimitRatio:         2.0,
				LimitSeedTime:      100,
			},
			want: map[string]string{
				"paused":             "false",
				"stopped":            "false",
				"skip_checking":      "true",
				"root_folder":        "false",
				"contentLayout":      "NoSubfolder",
				"autoTMM":            "false",
				"firstLastPiecePrio": "false",
				"ratioLimit":         "2.00",
				"savepath":           "/home/test/torrents",
				"seedingTimeLimit":   "100",
				"category":           "test",
				"tags":               "limited,slow",
				"upLimit":            "102400000",
				"dlLimit":            "102400000",
			},
		},
		{
			name: "test_04",
			fields: fields{
				Paused:             false,
				SkipHashCheck:      true,
				ContentLayout:      ContentLayoutOriginal,
				SavePath:           "/home/test/torrents",
				AutoTMM:            false,
				Category:           "test",
				Tags:               "limited,slow",
				LimitUploadSpeed:   100000,
				LimitDownloadSpeed: 100000,
				LimitRatio:         2.0,
				LimitSeedTime:      100,
			},
			want: map[string]string{
				"paused":             "false",
				"stopped":            "false",
				"skip_checking":      "true",
				"autoTMM":            "false",
				"firstLastPiecePrio": "false",
				"ratioLimit":         "2.00",
				"savepath":           "/home/test/torrents",
				"seedingTimeLimit":   "100",
				"category":           "test",
				"tags":               "limited,slow",
				"upLimit":            "102400000",
				"dlLimit":            "102400000",
			},
		},
		{
			name: "test_05",
			fields: fields{
				Paused:             false,
				SkipHashCheck:      true,
				ContentLayout:      ContentLayoutOriginal,
				SavePath:           "/home/test/torrents",
				AutoTMM:            false,
				Category:           "test",
				Tags:               "limited,slow",
				LimitUploadSpeed:   100000,
				LimitDownloadSpeed: 100000,
				LimitRatio:         2.0,
				LimitSeedTime:      100,
				Rename:             "test-torrent-rename",
			},
			want: map[string]string{
				"paused":             "false",
				"stopped":            "false",
				"skip_checking":      "true",
				"autoTMM":            "false",
				"firstLastPiecePrio": "false",
				"ratioLimit":         "2.00",
				"savepath":           "/home/test/torrents",
				"seedingTimeLimit":   "100",
				"category":           "test",
				"tags":               "limited,slow",
				"upLimit":            "102400000",
				"dlLimit":            "102400000",
				"rename":             "test-torrent-rename",
			},
		},
		{
			name: "test_06",
			fields: fields{
				Paused:             false,
				SkipHashCheck:      true,
				ContentLayout:      ContentLayoutOriginal,
				SavePath:           "/home/test/torrents",
				AutoTMM:            false,
				FirstLastPiecePrio: true,
				Category:           "test",
				Tags:               "limited,slow",
				LimitUploadSpeed:   100000,
				LimitDownloadSpeed: 100000,
				LimitRatio:         2.0,
				LimitSeedTime:      100,
				Rename:             "test-torrent-rename",
			},
			want: map[string]string{
				"paused":             "false",
				"stopped":            "false",
				"skip_checking":      "true",
				"autoTMM":            "false",
				"firstLastPiecePrio": "true",
				"ratioLimit":         "2.00",
				"savepath":           "/home/test/torrents",
				"seedingTimeLimit":   "100",
				"category":           "test",
				"tags":               "limited,slow",
				"upLimit":            "102400000",
				"dlLimit":            "102400000",
				"rename":             "test-torrent-rename",
			},
		},
		// Test paused and download path
		{
			name: "test_07",
			fields: fields{
				Paused:       true,
				DownloadPath: "/home/test/torrents",
				// This should get overriden because DownloadPath is set
				AutoTMM: true,
			},
			want: map[string]string{
				"paused":             "true",
				"stopped":            "true",
				"firstLastPiecePrio": "false",
				"autoTMM":            "false",
				"downloadPath":       "/home/test/torrents",
				"useDownloadPath":    "true",
			},
		},
		// Test stopped
		{
			name: "test_08",
			fields: fields{
				Stopped: true,
			},
			want: map[string]string{
				"paused":             "true",
				"stopped":            "true",
				"firstLastPiecePrio": "false",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &TorrentAddOptions{
				Paused:             tt.fields.Paused,
				Stopped:            tt.fields.Stopped,
				SkipHashCheck:      tt.fields.SkipHashCheck,
				ContentLayout:      tt.fields.ContentLayout,
				SavePath:           tt.fields.SavePath,
				DownloadPath:       tt.fields.DownloadPath,
				AutoTMM:            tt.fields.AutoTMM,
				Category:           tt.fields.Category,
				Tags:               tt.fields.Tags,
				LimitUploadSpeed:   tt.fields.LimitUploadSpeed,
				LimitDownloadSpeed: tt.fields.LimitDownloadSpeed,
				LimitRatio:         tt.fields.LimitRatio,
				LimitSeedTime:      tt.fields.LimitSeedTime,
				Rename:             tt.fields.Rename,
				FirstLastPiecePrio: tt.fields.FirstLastPiecePrio,
			}

			got := o.Prepare()
			assert.Equal(t, tt.want, got)
		})
	}
}
