package qbittorrent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRSSItems_ParseFeeds(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		wantLen  int
		wantURLs []string
	}{
		{
			name: "single feed",
			jsonData: `{
				"My Feed": {
					"uid": "abc123",
					"url": "https://example.com/rss",
					"title": "Example Feed"
				}
			}`,
			wantLen:  1,
			wantURLs: []string{"https://example.com/rss"},
		},
		{
			name: "nested folder with feeds",
			jsonData: `{
				"TV Shows": {
					"Feed 1": {
						"uid": "feed1",
						"url": "https://example.com/tv1"
					},
					"Feed 2": {
						"uid": "feed2",
						"url": "https://example.com/tv2"
					}
				},
				"Movies": {
					"uid": "movies",
					"url": "https://example.com/movies"
				}
			}`,
			wantLen:  3,
			wantURLs: []string{"https://example.com/tv1", "https://example.com/tv2", "https://example.com/movies"},
		},
		{
			name: "deeply nested folders",
			jsonData: `{
				"Level1": {
					"Level2": {
						"Deep Feed": {
							"uid": "deep",
							"url": "https://example.com/deep"
						}
					}
				}
			}`,
			wantLen:  1,
			wantURLs: []string{"https://example.com/deep"},
		},
		{
			name:     "empty",
			jsonData: `{}`,
			wantLen:  0,
			wantURLs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var items RSSItems
			err := json.Unmarshal([]byte(tt.jsonData), &items)
			require.NoError(t, err)

			feeds, err := items.ParseFeeds()
			require.NoError(t, err)
			assert.Len(t, feeds, tt.wantLen)

			if tt.wantLen > 0 {
				urls := make([]string, len(feeds))
				for i, f := range feeds {
					urls[i] = f.URL
				}
				assert.ElementsMatch(t, tt.wantURLs, urls)
			}
		})
	}
}

func TestIsFeed(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		want     bool
	}{
		{
			name:     "valid feed with URL",
			jsonData: `{"uid": "abc", "url": "https://example.com/rss"}`,
			want:     true,
		},
		{
			name:     "folder (no URL)",
			jsonData: `{"subfolder": {"uid": "xyz", "url": "https://example.com"}}`,
			want:     false,
		},
		{
			name:     "empty object",
			jsonData: `{}`,
			want:     false,
		},
		{
			name:     "feed with empty URL",
			jsonData: `{"uid": "abc", "url": ""}`,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFeed(json.RawMessage(tt.jsonData))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRSSFeed_Unmarshal(t *testing.T) {
	jsonData := `{
		"uid": "abc123",
		"url": "https://example.com/rss",
		"refreshInterval": 1800,
		"title": "Test Feed",
		"lastBuildDate": "Mon, 01 Jan 2024 12:00:00 +0000",
		"isLoading": false,
		"hasError": false,
		"articles": [
			{
				"id": "article1",
				"date": "Mon, 01 Jan 2024 10:00:00 +0000",
				"title": "Test Article",
				"author": "Author Name",
				"description": "Article description",
				"torrentURL": "https://example.com/torrent",
				"link": "https://example.com/article",
				"isRead": false
			}
		]
	}`

	var feed RSSFeed
	err := json.Unmarshal([]byte(jsonData), &feed)
	require.NoError(t, err)

	assert.Equal(t, "abc123", feed.UID)
	assert.Equal(t, "https://example.com/rss", feed.URL)
	assert.Equal(t, int64(1800), feed.RefreshInterval)
	assert.Equal(t, "Test Feed", feed.Title)
	assert.False(t, feed.IsLoading)
	assert.False(t, feed.HasError)
	assert.Len(t, feed.Articles, 1)

	article := feed.Articles[0]
	assert.Equal(t, "article1", article.ID)
	assert.Equal(t, "Test Article", article.Title)
	assert.Equal(t, "Author Name", article.Author)
	assert.Equal(t, "https://example.com/torrent", article.TorrentURL)
	assert.False(t, article.IsRead)
}

func TestRSSAutoDownloadRule_Unmarshal(t *testing.T) {
	jsonData := `{
		"enabled": true,
		"priority": 1,
		"useRegex": true,
		"mustContain": "1080p",
		"mustNotContain": "CAM|TS",
		"episodeFilter": "S01-S05",
		"affectedFeeds": ["https://example.com/rss1", "https://example.com/rss2"],
		"lastMatch": "Mon, 01 Jan 2024 12:00:00 +0000",
		"ignoreDays": 7,
		"smartFilter": true,
		"previouslyMatchedEpisodes": ["S01E01", "S01E02"],
		"torrentParams": {
			"category": "tv-shows",
			"tags": ["hd", "auto"],
			"save_path": "/downloads/tv"
		}
	}`

	var rule RSSAutoDownloadRule
	err := json.Unmarshal([]byte(jsonData), &rule)
	require.NoError(t, err)

	assert.True(t, rule.Enabled)
	assert.Equal(t, 1, rule.Priority)
	assert.True(t, rule.UseRegex)
	assert.Equal(t, "1080p", rule.MustContain)
	assert.Equal(t, "CAM|TS", rule.MustNotContain)
	assert.Equal(t, "S01-S05", rule.EpisodeFilter)
	assert.Len(t, rule.AffectedFeeds, 2)
	assert.Equal(t, 7, rule.IgnoreDays)
	assert.True(t, rule.SmartFilter)
	assert.Len(t, rule.PreviouslyMatchedEpisodes, 2)

	require.NotNil(t, rule.TorrentParams)
	assert.Equal(t, "tv-shows", rule.TorrentParams.Category)
	assert.Equal(t, "/downloads/tv", rule.TorrentParams.SavePath)
	assert.ElementsMatch(t, []string{"hd", "auto"}, rule.TorrentParams.Tags)
}

func TestRSSAutoDownloadRule_Marshal(t *testing.T) {
	rule := RSSAutoDownloadRule{
		Enabled:        true,
		Priority:       0,
		UseRegex:       false,
		MustContain:    "720p",
		MustNotContain: "",
		AffectedFeeds:  []string{"https://example.com/rss"},
		SmartFilter:    true,
		TorrentParams: &RSSRuleTorrentParams{
			Category: "movies",
			SavePath: "/downloads/movies",
		},
	}

	data, err := json.Marshal(rule)
	require.NoError(t, err)

	// Unmarshal back to verify round-trip
	var decoded RSSAutoDownloadRule
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, rule.Enabled, decoded.Enabled)
	assert.Equal(t, rule.Priority, decoded.Priority)
	assert.Equal(t, rule.UseRegex, decoded.UseRegex)
	assert.Equal(t, rule.MustContain, decoded.MustContain)
	assert.Equal(t, rule.AffectedFeeds, decoded.AffectedFeeds)
	assert.Equal(t, rule.SmartFilter, decoded.SmartFilter)

	require.NotNil(t, decoded.TorrentParams)
	assert.Equal(t, rule.TorrentParams.Category, decoded.TorrentParams.Category)
	assert.Equal(t, rule.TorrentParams.SavePath, decoded.TorrentParams.SavePath)
}

func TestRSSRules_Unmarshal(t *testing.T) {
	jsonData := `{
		"TV Rule": {
			"enabled": true,
			"priority": 0,
			"mustContain": "1080p",
			"affectedFeeds": ["https://example.com/tv"]
		},
		"Movie Rule": {
			"enabled": false,
			"priority": 1,
			"mustContain": "2160p",
			"affectedFeeds": ["https://example.com/movies"]
		}
	}`

	var rules RSSRules
	err := json.Unmarshal([]byte(jsonData), &rules)
	require.NoError(t, err)

	assert.Len(t, rules, 2)

	tvRule, ok := rules["TV Rule"]
	require.True(t, ok)
	assert.True(t, tvRule.Enabled)
	assert.Equal(t, "1080p", tvRule.MustContain)

	movieRule, ok := rules["Movie Rule"]
	require.True(t, ok)
	assert.False(t, movieRule.Enabled)
	assert.Equal(t, "2160p", movieRule.MustContain)
}

func TestRSSMatchingArticles_Unmarshal(t *testing.T) {
	jsonData := `{
		"Feed 1": ["Article A", "Article B"],
		"Feed 2": ["Article C"]
	}`

	var articles RSSMatchingArticles
	err := json.Unmarshal([]byte(jsonData), &articles)
	require.NoError(t, err)

	assert.Len(t, articles, 2)
	assert.ElementsMatch(t, []string{"Article A", "Article B"}, articles["Feed 1"])
	assert.ElementsMatch(t, []string{"Article C"}, articles["Feed 2"])
}
