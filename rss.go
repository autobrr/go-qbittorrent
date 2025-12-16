package qbittorrent

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/autobrr/go-qbittorrent/errors"
)

// RSS Domain Types

// RSSItems represents the hierarchical response from rss/items endpoint.
// The response is a map where keys are item names and values can be either
// RSSFeed objects or nested RSSItems (for folders).
type RSSItems map[string]json.RawMessage

// RSSFeed represents an RSS feed with optional article data.
type RSSFeed struct {
	UID             string       `json:"uid"`
	URL             string       `json:"url"`
	RefreshInterval int64        `json:"refreshInterval,omitempty"`
	Title           string       `json:"title,omitempty"`
	LastBuildDate   string       `json:"lastBuildDate,omitempty"`
	IsLoading       bool         `json:"isLoading,omitempty"`
	HasError        bool         `json:"hasError,omitempty"`
	Articles        []RSSArticle `json:"articles,omitempty"`
}

// RSSArticle represents an RSS feed article.
type RSSArticle struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Title       string `json:"title"`
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
	TorrentURL  string `json:"torrentURL,omitempty"`
	Link        string `json:"link,omitempty"`
	IsRead      bool   `json:"isRead"`
}

// RSSAutoDownloadRule represents an RSS auto-download rule.
type RSSAutoDownloadRule struct {
	Enabled                   bool                  `json:"enabled"`
	Priority                  int                   `json:"priority"`
	UseRegex                  bool                  `json:"useRegex"`
	MustContain               string                `json:"mustContain"`
	MustNotContain            string                `json:"mustNotContain"`
	EpisodeFilter             string                `json:"episodeFilter,omitempty"`
	AffectedFeeds             []string              `json:"affectedFeeds"`
	LastMatch                 string                `json:"lastMatch,omitempty"`
	IgnoreDays                int                   `json:"ignoreDays"`
	SmartFilter               bool                  `json:"smartFilter"`
	PreviouslyMatchedEpisodes []string              `json:"previouslyMatchedEpisodes,omitempty"`
	TorrentParams             *RSSRuleTorrentParams `json:"torrentParams,omitempty"`
	// Legacy fields for backward compatibility
	AddPaused            *bool  `json:"addPaused,omitempty"`
	SavePath             string `json:"savePath,omitempty"`
	AssignedCategory     string `json:"assignedCategory,omitempty"`
	TorrentContentLayout string `json:"torrentContentLayout,omitempty"`
}

// RSSRuleTorrentParams represents torrent parameters for an auto-download rule.
// JSON field names use snake_case to match qBittorrent's AddTorrentParams serialization.
type RSSRuleTorrentParams struct {
	Category                 string   `json:"category,omitempty"`
	Tags                     []string `json:"tags,omitempty"`
	SavePath                 string   `json:"save_path,omitempty"`
	DownloadPath             string   `json:"download_path,omitempty"`
	ContentLayout            string   `json:"content_layout,omitempty"`
	OperatingMode            string   `json:"operating_mode,omitempty"`
	SkipChecking             bool     `json:"skip_checking,omitempty"`
	UploadLimit              int      `json:"upload_limit,omitempty"`
	DownloadLimit            int      `json:"download_limit,omitempty"`
	SeedingTimeLimit         int      `json:"seeding_time_limit,omitempty"`
	InactiveSeedingTimeLimit int      `json:"inactive_seeding_time_limit,omitempty"`
	ShareLimitAction         string   `json:"share_limit_action,omitempty"`
	RatioLimit               float64  `json:"ratio_limit,omitempty"`
	Stopped                  *bool    `json:"stopped,omitempty"`
	StopCondition            string   `json:"stop_condition,omitempty"`
	UseAutoTMM               *bool    `json:"use_auto_tmm,omitempty"`
	UseDownloadPath          *bool    `json:"use_download_path,omitempty"`
	AddToQueueTop            *bool    `json:"add_to_top_of_queue,omitempty"`
}

// RSSRules represents the response from rss/rules endpoint.
// Keys are rule names, values are the rule definitions.
type RSSRules map[string]RSSAutoDownloadRule

// RSSMatchingArticles represents the response from rss/matchingArticles endpoint.
// Keys are feed names, values are arrays of matching article titles.
type RSSMatchingArticles map[string][]string

// ParseFeeds parses the hierarchical RSSItems response and returns all feeds.
func (items RSSItems) ParseFeeds() ([]RSSFeed, error) {
	var feeds []RSSFeed
	for _, raw := range items {
		var feed RSSFeed
		if err := json.Unmarshal(raw, &feed); err == nil && feed.URL != "" {
			feeds = append(feeds, feed)
			continue
		}
		// Try parsing as nested folder
		var nested RSSItems
		if err := json.Unmarshal(raw, &nested); err == nil {
			nestedFeeds, _ := nested.ParseFeeds()
			feeds = append(feeds, nestedFeeds...)
		}
	}
	return feeds, nil
}

// IsFeed checks if the raw JSON represents a feed (has URL field).
func IsFeed(raw json.RawMessage) bool {
	var feed RSSFeed
	if err := json.Unmarshal(raw, &feed); err == nil {
		return feed.URL != ""
	}
	return false
}

// RSS Methods

// GetRSSItems retrieves all RSS feeds and folders.
// If withData is true, includes article data for each feed.
func (c *Client) GetRSSItems(withData bool) (RSSItems, error) {
	return c.GetRSSItemsCtx(context.Background(), withData)
}

// GetRSSItemsCtx retrieves all RSS feeds and folders with context.
func (c *Client) GetRSSItemsCtx(ctx context.Context, withData bool) (RSSItems, error) {
	opts := map[string]string{}
	if withData {
		opts["withData"] = "true"
	}

	resp, err := c.getCtx(ctx, "rss/items", opts)
	if err != nil {
		return nil, errors.Wrap(err, "could not get RSS items")
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrap(ErrUnexpectedStatus, "could not get RSS items; status code: %d", resp.StatusCode)
	}

	var items RSSItems
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return items, nil
}

// AddRSSFolder creates a new RSS folder.
// Path uses backslash as separator (e.g., "Folder\\Subfolder").
func (c *Client) AddRSSFolder(path string) error {
	return c.AddRSSFolderCtx(context.Background(), path)
}

// AddRSSFolderCtx creates a new RSS folder with context.
func (c *Client) AddRSSFolderCtx(ctx context.Context, path string) error {
	opts := map[string]string{
		"path": path,
	}

	resp, err := c.postCtx(ctx, "rss/addFolder", opts)
	if err != nil {
		return errors.Wrap(err, "could not add RSS folder; path: %s", path)
	}

	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusConflict {
		return errors.Wrap(ErrRSSPathConflict, "path: %s", path)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not add RSS folder; path: %s | status code: %d", path, resp.StatusCode)
	}

	return nil
}

// AddRSSFeed adds a new RSS feed.
// refreshInterval is in seconds; 0 means use global default.
func (c *Client) AddRSSFeed(url, path string, refreshInterval int64) error {
	return c.AddRSSFeedCtx(context.Background(), url, path, refreshInterval)
}

// AddRSSFeedCtx adds a new RSS feed with context.
func (c *Client) AddRSSFeedCtx(ctx context.Context, url, path string, refreshInterval int64) error {
	opts := map[string]string{
		"url":  url,
		"path": path,
	}
	if refreshInterval > 0 {
		opts["refreshInterval"] = strconv.FormatInt(refreshInterval, 10)
	}

	resp, err := c.postCtx(ctx, "rss/addFeed", opts)
	if err != nil {
		return errors.Wrap(err, "could not add RSS feed; url: %s", url)
	}

	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusConflict {
		return errors.Wrap(ErrRSSPathConflict, "path: %s", path)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not add RSS feed; url: %s | status code: %d", url, resp.StatusCode)
	}

	return nil
}

// SetRSSFeedURL changes the URL of an existing feed.
func (c *Client) SetRSSFeedURL(path, url string) error {
	return c.SetRSSFeedURLCtx(context.Background(), path, url)
}

// SetRSSFeedURLCtx changes the URL of an existing feed with context.
func (c *Client) SetRSSFeedURLCtx(ctx context.Context, path, url string) error {
	opts := map[string]string{
		"path": path,
		"url":  url,
	}

	resp, err := c.postCtx(ctx, "rss/setFeedURL", opts)
	if err != nil {
		return errors.Wrap(err, "could not set RSS feed URL; path: %s", path)
	}

	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusConflict {
		return errors.Wrap(ErrRSSItemNotFound, "path: %s", path)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not set RSS feed URL; path: %s | status code: %d", path, resp.StatusCode)
	}

	return nil
}

// SetRSSFeedRefreshInterval sets the refresh interval for a feed.
// refreshInterval is in seconds.
func (c *Client) SetRSSFeedRefreshInterval(path string, refreshInterval int64) error {
	return c.SetRSSFeedRefreshIntervalCtx(context.Background(), path, refreshInterval)
}

// SetRSSFeedRefreshIntervalCtx sets the refresh interval for a feed with context.
func (c *Client) SetRSSFeedRefreshIntervalCtx(ctx context.Context, path string, refreshInterval int64) error {
	opts := map[string]string{
		"path":            path,
		"refreshInterval": strconv.FormatInt(refreshInterval, 10),
	}

	resp, err := c.postCtx(ctx, "rss/setFeedRefreshInterval", opts)
	if err != nil {
		return errors.Wrap(err, "could not set RSS feed refresh interval; path: %s", path)
	}

	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusConflict {
		return errors.Wrap(ErrRSSItemNotFound, "path: %s", path)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not set RSS feed refresh interval; path: %s | status code: %d", path, resp.StatusCode)
	}

	return nil
}

// RemoveRSSItem removes a feed or folder.
func (c *Client) RemoveRSSItem(path string) error {
	return c.RemoveRSSItemCtx(context.Background(), path)
}

// RemoveRSSItemCtx removes a feed or folder with context.
func (c *Client) RemoveRSSItemCtx(ctx context.Context, path string) error {
	opts := map[string]string{
		"path": path,
	}

	resp, err := c.postCtx(ctx, "rss/removeItem", opts)
	if err != nil {
		return errors.Wrap(err, "could not remove RSS item; path: %s", path)
	}

	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusConflict {
		return errors.Wrap(ErrRSSItemNotFound, "path: %s", path)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not remove RSS item; path: %s | status code: %d", path, resp.StatusCode)
	}

	return nil
}

// MoveRSSItem moves a feed or folder to a new location.
func (c *Client) MoveRSSItem(itemPath, destPath string) error {
	return c.MoveRSSItemCtx(context.Background(), itemPath, destPath)
}

// MoveRSSItemCtx moves a feed or folder to a new location with context.
func (c *Client) MoveRSSItemCtx(ctx context.Context, itemPath, destPath string) error {
	opts := map[string]string{
		"itemPath": itemPath,
		"destPath": destPath,
	}

	resp, err := c.postCtx(ctx, "rss/moveItem", opts)
	if err != nil {
		return errors.Wrap(err, "could not move RSS item; itemPath: %s", itemPath)
	}

	defer drainAndClose(resp)

	// qBittorrent returns 409 Conflict for both "item not found" and "dest already exists"
	if resp.StatusCode == http.StatusConflict {
		return errors.Wrap(ErrRSSPathConflict, "itemPath: %s, destPath: %s", itemPath, destPath)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not move RSS item; itemPath: %s | status code: %d", itemPath, resp.StatusCode)
	}

	return nil
}

// RefreshRSSItem triggers a manual refresh of a feed or all feeds in a folder.
func (c *Client) RefreshRSSItem(itemPath string) error {
	return c.RefreshRSSItemCtx(context.Background(), itemPath)
}

// RefreshRSSItemCtx triggers a manual refresh of a feed or all feeds in a folder with context.
// Note: qBittorrent silently returns 200 OK even for invalid paths.
func (c *Client) RefreshRSSItemCtx(ctx context.Context, itemPath string) error {
	opts := map[string]string{
		"itemPath": itemPath,
	}

	resp, err := c.postCtx(ctx, "rss/refreshItem", opts)
	if err != nil {
		return errors.Wrap(err, "could not refresh RSS item; itemPath: %s", itemPath)
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not refresh RSS item; itemPath: %s | status code: %d", itemPath, resp.StatusCode)
	}

	return nil
}

// MarkRSSItemAsRead marks all articles in an item (feed/folder) as read.
// If articleID is provided, only that specific article is marked as read.
func (c *Client) MarkRSSItemAsRead(itemPath string, articleID string) error {
	return c.MarkRSSItemAsReadCtx(context.Background(), itemPath, articleID)
}

// MarkRSSItemAsReadCtx marks articles as read with context.
// Note: qBittorrent silently returns 200 OK even for invalid paths.
func (c *Client) MarkRSSItemAsReadCtx(ctx context.Context, itemPath string, articleID string) error {
	opts := map[string]string{
		"itemPath": itemPath,
	}
	if articleID != "" {
		opts["articleId"] = articleID
	}

	resp, err := c.postCtx(ctx, "rss/markAsRead", opts)
	if err != nil {
		return errors.Wrap(err, "could not mark RSS item as read; itemPath: %s", itemPath)
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not mark RSS item as read; itemPath: %s | status code: %d", itemPath, resp.StatusCode)
	}

	return nil
}

// GetRSSRules retrieves all RSS auto-download rules.
func (c *Client) GetRSSRules() (RSSRules, error) {
	return c.GetRSSRulesCtx(context.Background())
}

// GetRSSRulesCtx retrieves all RSS auto-download rules with context.
func (c *Client) GetRSSRulesCtx(ctx context.Context) (RSSRules, error) {
	resp, err := c.getCtx(ctx, "rss/rules", nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not get RSS rules")
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrap(ErrUnexpectedStatus, "could not get RSS rules; status code: %d", resp.StatusCode)
	}

	var rules RSSRules
	if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return rules, nil
}

// SetRSSRule creates or updates an auto-download rule.
func (c *Client) SetRSSRule(ruleName string, rule RSSAutoDownloadRule) error {
	return c.SetRSSRuleCtx(context.Background(), ruleName, rule)
}

// SetRSSRuleCtx creates or updates an auto-download rule with context.
func (c *Client) SetRSSRuleCtx(ctx context.Context, ruleName string, rule RSSAutoDownloadRule) error {
	ruleDef, err := json.Marshal(rule)
	if err != nil {
		return errors.Wrap(err, "could not marshal rule definition")
	}

	opts := map[string]string{
		"ruleName": ruleName,
		"ruleDef":  string(ruleDef),
	}

	resp, err := c.postCtx(ctx, "rss/setRule", opts)
	if err != nil {
		return errors.Wrap(err, "could not set RSS rule; ruleName: %s", ruleName)
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not set RSS rule; ruleName: %s | status code: %d", ruleName, resp.StatusCode)
	}

	return nil
}

// RenameRSSRule renames an existing rule.
func (c *Client) RenameRSSRule(ruleName, newRuleName string) error {
	return c.RenameRSSRuleCtx(context.Background(), ruleName, newRuleName)
}

// RenameRSSRuleCtx renames an existing rule with context.
// Note: qBittorrent silently succeeds even if the rule doesn't exist.
func (c *Client) RenameRSSRuleCtx(ctx context.Context, ruleName, newRuleName string) error {
	opts := map[string]string{
		"ruleName":    ruleName,
		"newRuleName": newRuleName,
	}

	resp, err := c.postCtx(ctx, "rss/renameRule", opts)
	if err != nil {
		return errors.Wrap(err, "could not rename RSS rule; ruleName: %s", ruleName)
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not rename RSS rule; ruleName: %s | status code: %d", ruleName, resp.StatusCode)
	}

	return nil
}

// RemoveRSSRule deletes an auto-download rule.
func (c *Client) RemoveRSSRule(ruleName string) error {
	return c.RemoveRSSRuleCtx(context.Background(), ruleName)
}

// RemoveRSSRuleCtx deletes an auto-download rule with context.
// Note: qBittorrent silently succeeds even if the rule doesn't exist.
func (c *Client) RemoveRSSRuleCtx(ctx context.Context, ruleName string) error {
	opts := map[string]string{
		"ruleName": ruleName,
	}

	resp, err := c.postCtx(ctx, "rss/removeRule", opts)
	if err != nil {
		return errors.Wrap(err, "could not remove RSS rule; ruleName: %s", ruleName)
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(ErrUnexpectedStatus, "could not remove RSS rule; ruleName: %s | status code: %d", ruleName, resp.StatusCode)
	}

	return nil
}

// GetRSSMatchingArticles gets articles matching a rule for preview.
func (c *Client) GetRSSMatchingArticles(ruleName string) (RSSMatchingArticles, error) {
	return c.GetRSSMatchingArticlesCtx(context.Background(), ruleName)
}

// GetRSSMatchingArticlesCtx gets articles matching a rule for preview with context.
// Note: qBittorrent returns an empty object for non-existent rules.
func (c *Client) GetRSSMatchingArticlesCtx(ctx context.Context, ruleName string) (RSSMatchingArticles, error) {
	opts := map[string]string{
		"ruleName": ruleName,
	}

	resp, err := c.getCtx(ctx, "rss/matchingArticles", opts)
	if err != nil {
		return nil, errors.Wrap(err, "could not get RSS matching articles; ruleName: %s", ruleName)
	}

	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrap(ErrUnexpectedStatus, "could not get RSS matching articles; ruleName: %s | status code: %d", ruleName, resp.StatusCode)
	}

	var articles RSSMatchingArticles
	if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return articles, nil
}
