package qbittorrent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTorrentAddOptions_Prepare(t *testing.T) {
	type fields struct {
		Paused                   bool
		SkipHashCheck            bool
		ContentLayout            ContentLayout
		SavePath                 string
		AutoTMM                  bool
		Category                 string
		Tags                     string
		LimitUploadSpeed         int64
		LimitDownloadSpeed       int64
		LimitRatio               float64
		LimitSeedTime            int64
		Rename                   string
		ToggleFirstLastPiecePrio bool
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
				"paused":                   "false",
				"skip_checking":            "true",
				"autoTMM":                  "false",
				"toggleFirstLastPiecePrio": "false",
				"ratioLimit":               "2.00",
				"savepath":                 "/home/test/torrents",
				"seedingTimeLimit":         "100",
				"category":                 "test",
				"tags":                     "limited,slow",
				"upLimit":                  "102400000",
				"dlLimit":                  "102400000",
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
				"paused":                   "false",
				"skip_checking":            "true",
				"root_folder":              "true",
				"contentLayout":            "Subfolder",
				"autoTMM":                  "false",
				"toggleFirstLastPiecePrio": "false",
				"ratioLimit":               "2.00",
				"savepath":                 "/home/test/torrents",
				"seedingTimeLimit":         "100",
				"category":                 "test",
				"tags":                     "limited,slow",
				"upLimit":                  "102400000",
				"dlLimit":                  "102400000",
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
				"paused":                   "false",
				"skip_checking":            "true",
				"root_folder":              "false",
				"contentLayout":            "NoSubfolder",
				"autoTMM":                  "false",
				"toggleFirstLastPiecePrio": "false",
				"ratioLimit":               "2.00",
				"savepath":                 "/home/test/torrents",
				"seedingTimeLimit":         "100",
				"category":                 "test",
				"tags":                     "limited,slow",
				"upLimit":                  "102400000",
				"dlLimit":                  "102400000",
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
				"paused":                   "false",
				"skip_checking":            "true",
				"autoTMM":                  "false",
				"toggleFirstLastPiecePrio": "false",
				"ratioLimit":               "2.00",
				"savepath":                 "/home/test/torrents",
				"seedingTimeLimit":         "100",
				"category":                 "test",
				"tags":                     "limited,slow",
				"upLimit":                  "102400000",
				"dlLimit":                  "102400000",
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
				"paused":                   "false",
				"skip_checking":            "true",
				"autoTMM":                  "false",
				"toggleFirstLastPiecePrio": "false",
				"ratioLimit":               "2.00",
				"savepath":                 "/home/test/torrents",
				"seedingTimeLimit":         "100",
				"category":                 "test",
				"tags":                     "limited,slow",
				"upLimit":                  "102400000",
				"dlLimit":                  "102400000",
				"rename":                   "test-torrent-rename",
			},
		},
		{
			name: "test_06",
			fields: fields{
				Paused:                   false,
				SkipHashCheck:            true,
				ContentLayout:            ContentLayoutOriginal,
				SavePath:                 "/home/test/torrents",
				AutoTMM:                  false,
				ToggleFirstLastPiecePrio: true,
				Category:                 "test",
				Tags:                     "limited,slow",
				LimitUploadSpeed:         100000,
				LimitDownloadSpeed:       100000,
				LimitRatio:               2.0,
				LimitSeedTime:            100,
				Rename:                   "test-torrent-rename",
			},
			want: map[string]string{
				"paused":                   "false",
				"skip_checking":            "true",
				"autoTMM":                  "false",
				"toggleFirstLastPiecePrio": "true",
				"ratioLimit":               "2.00",
				"savepath":                 "/home/test/torrents",
				"seedingTimeLimit":         "100",
				"category":                 "test",
				"tags":                     "limited,slow",
				"upLimit":                  "102400000",
				"dlLimit":                  "102400000",
				"rename":                   "test-torrent-rename",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &TorrentAddOptions{
				Paused:                   tt.fields.Paused,
				SkipHashCheck:            tt.fields.SkipHashCheck,
				ContentLayout:            tt.fields.ContentLayout,
				SavePath:                 tt.fields.SavePath,
				AutoTMM:                  tt.fields.AutoTMM,
				Category:                 tt.fields.Category,
				Tags:                     tt.fields.Tags,
				LimitUploadSpeed:         tt.fields.LimitUploadSpeed,
				LimitDownloadSpeed:       tt.fields.LimitDownloadSpeed,
				LimitRatio:               tt.fields.LimitRatio,
				LimitSeedTime:            tt.fields.LimitSeedTime,
				Rename:                   tt.fields.Rename,
				ToggleFirstLastPiecePrio: tt.fields.ToggleFirstLastPiecePrio,
			}

			got := o.Prepare()
			assert.Equal(t, tt.want, got)
		})
	}
}
