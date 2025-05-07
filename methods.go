package qbittorrent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/go-qbittorrent/errors"

	"github.com/Masterminds/semver"
)

// Login https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-4.1)#authentication
func (c *Client) Login() error {
	return c.LoginCtx(context.Background())
}

func (c *Client) LoginCtx(ctx context.Context) error {
	if c.cfg.Username == "" && c.cfg.Password == "" {
		return nil
	}

	opts := map[string]string{
		"username": c.cfg.Username,
		"password": c.cfg.Password,
	}

	resp, err := c.postBasicCtx(ctx, "auth/login", opts)
	if err != nil {
		return errors.Wrap(err, "login error")
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return errors.New("User's IP is banned for too many failed login attempts")
	} else if resp.StatusCode != http.StatusOK { // check for correct status code
		return errors.New("qbittorrent login bad status %v", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	bodyString := string(bodyBytes)

	// read output
	if bodyString == "Fails." {
		return errors.New("bad credentials")
	}

	// good response == "Ok."

	// place cookies in jar for future requests
	if cookies := resp.Cookies(); len(cookies) > 0 {
		c.setCookies(cookies)
	} else if bodyString != "Ok." {
		return errors.New("bad credentials")
	}

	c.log.Printf("logged into client: %v", c.cfg.Host)

	return nil
}

func (c *Client) setApiVersion() error {
	versionString, err := c.GetWebAPIVersionCtx(context.Background())
	if err != nil {
		return errors.Wrap(err, "could not get webapi version")
	}

	c.log.Printf("webapi version: %v", versionString)

	ver, err := semver.NewVersion(versionString)
	if err != nil {
		return errors.Wrap(err, "could not parse webapi version")
	}

	c.version = ver

	return nil
}

func (c *Client) getApiVersion() (*semver.Version, error) {
	if c.version == nil || (c.version.Major() == 0 && c.version.Minor() == 0 && c.version.Patch() == 0) {
		err := c.setApiVersion()
		if err != nil {
			return nil, err
		}
	}

	return c.version, nil
}

func (c *Client) GetAppPreferences() (AppPreferences, error) {
	return c.GetAppPreferencesCtx(context.Background())
}

func (c *Client) GetAppPreferencesCtx(ctx context.Context) (AppPreferences, error) {
	var app AppPreferences
	resp, err := c.getCtx(ctx, "app/preferences", nil)
	if err != nil {
		return app, errors.Wrap(err, "could not get app preferences")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return app, errors.Wrap(err, "could not read body")
	}

	if err := json.Unmarshal(body, &app); err != nil {
		return app, errors.Wrap(err, "could not unmarshal body")
	}

	return app, nil
}

func (c *Client) SetPreferences(prefs map[string]interface{}) error {
	return c.SetPreferencesCtx(context.Background(), prefs)
}

func (c *Client) SetPreferencesCtx(ctx context.Context, prefs map[string]interface{}) error {
	prefsJSON, err := json.Marshal(prefs)
	if err != nil {
		return errors.Wrap(err, "could not marshal preferences")
	}

	data := map[string]string{
		"json": string(prefsJSON),
	}

	resp, err := c.postCtx(ctx, "app/setPreferences", data)
	if err != nil {
		return errors.Wrap(err, "could not set preferences")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("unexpected status when setting preferences: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) GetTorrents(o TorrentFilterOptions) ([]Torrent, error) {
	return c.GetTorrentsCtx(context.Background(), o)
}

func (c *Client) GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error) {
	opts := map[string]string{}

	if o.Reverse {
		opts["reverse"] = strconv.FormatBool(o.Reverse)
	}

	if o.Limit > 0 {
		opts["limit"] = strconv.Itoa(o.Limit)
	}

	if o.Offset > 0 {
		opts["offset"] = strconv.Itoa(o.Offset)
	}

	if o.Sort != "" {
		opts["sort"] = o.Sort
	}

	if o.Filter != "" {
		opts["filter"] = string(o.Filter)
	}

	if o.Category != "" {
		opts["category"] = o.Category
	}

	if o.Tag != "" {
		opts["tag"] = o.Tag
	}

	if len(o.Hashes) > 0 {
		opts["hashes"] = strings.Join(o.Hashes, "|")
	}

	// qbit v5.1+
	if o.IncludeTrackers {
		opts["includeTrackers"] = strconv.FormatBool(o.IncludeTrackers)
	}

	resp, err := c.getCtx(ctx, "torrents/info", opts)
	if err != nil {
		return nil, errors.Wrap(err, "get torrents error")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "could not read body")
	}

	var torrents []Torrent
	if err := json.Unmarshal(body, &torrents); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return torrents, nil
}

func (c *Client) GetTorrentsActiveDownloads() ([]Torrent, error) {
	return c.GetTorrentsActiveDownloadsCtx(context.Background())
}

func (c *Client) GetTorrentsActiveDownloadsCtx(ctx context.Context) ([]Torrent, error) {
	torrents, err := c.GetTorrentsCtx(ctx, TorrentFilterOptions{Filter: TorrentFilterDownloading})
	if err != nil {
		return nil, err
	}

	res := make([]Torrent, 0)
	for _, torrent := range torrents {
		// qbit counts paused torrents as downloading as well by default
		// so only add torrents with state downloading, and not pausedDl, stalledDl etc
		if torrent.State == TorrentStateDownloading || torrent.State == TorrentStateStalledDl {
			res = append(res, torrent)
		}
	}

	return res, nil
}

func (c *Client) GetTorrentProperties(hash string) (TorrentProperties, error) {
	return c.GetTorrentPropertiesCtx(context.Background(), hash)
}

func (c *Client) GetTorrentPropertiesCtx(ctx context.Context, hash string) (TorrentProperties, error) {
	opts := map[string]string{
		"hash": hash,
	}

	var prop TorrentProperties
	resp, err := c.getCtx(ctx, "torrents/properties", opts)
	if err != nil {
		return prop, errors.Wrap(err, "could not get app preferences")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return prop, errors.Wrap(err, "could not read body")
	}

	if err := json.Unmarshal(body, &prop); err != nil {
		return prop, errors.Wrap(err, "could not unmarshal body")
	}

	return prop, nil
}

func (c *Client) GetTorrentsRaw() (string, error) {
	return c.GetTorrentsRawCtx(context.Background())
}

func (c *Client) GetTorrentsRawCtx(ctx context.Context) (string, error) {
	resp, err := c.getCtx(ctx, "torrents/info", nil)
	if err != nil {
		return "", errors.Wrap(err, "could not get torrents raw")
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "could not get read body torrents raw")
	}

	return string(data), nil
}

func (c *Client) GetTorrentTrackers(hash string) ([]TorrentTracker, error) {
	return c.GetTorrentTrackersCtx(context.Background(), hash)
}

func (c *Client) GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error) {
	opts := map[string]string{
		"hash": hash,
	}

	resp, err := c.getCtx(ctx, "torrents/trackers", opts)
	if err != nil {
		return nil, errors.Wrap(err, "could not get torrent trackers for hash: %v", hash)
	}

	defer resp.Body.Close()

	dump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		//c.log.Printf("get torrent trackers error dump response: %v\n", string(dump))
		return nil, errors.Wrap(err, "could not dump response for hash: %v", hash)
	}

	c.log.Printf("get torrent trackers response dump: %q", dump)

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	} else if resp.StatusCode == http.StatusForbidden {
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "could not read body")
	}

	c.log.Printf("get torrent trackers body: %v\n", string(body))

	var trackers []TorrentTracker
	if err := json.Unmarshal(body, &trackers); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return trackers, nil
}

func (c *Client) AddTorrentFromMemory(buf []byte, options map[string]string) error {
	return c.AddTorrentFromMemoryCtx(context.Background(), buf, options)
}

func (c *Client) AddTorrentFromMemoryCtx(ctx context.Context, buf []byte, options map[string]string) error {

	res, err := c.postMemoryCtx(ctx, "torrents/add", buf, options)
	if err != nil {
		return errors.Wrap(err, "could not add torrent")
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return errors.New("could not add torrent, unexpected status: %v", res.StatusCode)
	}

	return nil
}

// AddTorrentFromFile add new torrent from torrent file
func (c *Client) AddTorrentFromFile(filePath string, options map[string]string) error {
	return c.AddTorrentFromFileCtx(context.Background(), filePath, options)
}

func (c *Client) AddTorrentFromFileCtx(ctx context.Context, filePath string, options map[string]string) error {

	res, err := c.postFileCtx(ctx, "torrents/add", filePath, options)
	if err != nil {
		return errors.Wrap(err, "could not add torrent %v", filePath)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return errors.New("could not add torrent %v unexpected status: %v", filePath, res.StatusCode)
	}

	return nil
}

// AddTorrentFromUrl add new torrent from torrent file
func (c *Client) AddTorrentFromUrl(url string, options map[string]string) error {
	return c.AddTorrentFromUrlCtx(context.Background(), url, options)
}

func (c *Client) AddTorrentFromUrlCtx(ctx context.Context, url string, options map[string]string) error {
	if url == "" {
		return errors.New("no torrent url provided")
	}

	options["urls"] = url

	res, err := c.postCtx(ctx, "torrents/add", options)
	if err != nil {
		return errors.Wrap(err, "could not add torrent %v", url)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return errors.New("could not add torrent %v unexpected status: %v", url, res.StatusCode)
	}

	return nil
}

func (c *Client) DeleteTorrents(hashes []string, deleteFiles bool) error {
	return c.DeleteTorrentsCtx(context.Background(), hashes, deleteFiles)
}

func (c *Client) DeleteTorrentsCtx(ctx context.Context, hashes []string, deleteFiles bool) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")

	opts := map[string]string{
		"hashes":      hv,
		"deleteFiles": strconv.FormatBool(deleteFiles),
	}

	resp, err := c.postCtx(ctx, "torrents/delete", opts)
	if err != nil {
		return errors.Wrap(err, "could not delete torrents: %+v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not delete torrents %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) ReAnnounceTorrents(hashes []string) error {
	return c.ReAnnounceTorrentsCtx(context.Background(), hashes)
}

func (c *Client) ReAnnounceTorrentsCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.postCtx(ctx, "torrents/reannounce", opts)
	if err != nil {
		return errors.Wrap(err, "could not re-announce torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not re-announce torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) GetTransferInfo() (*TransferInfo, error) {
	return c.GetTransferInfoCtx(context.Background())
}

func (c *Client) GetTransferInfoCtx(ctx context.Context) (*TransferInfo, error) {
	resp, err := c.getCtx(ctx, "transfer/info", nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not get transfer info")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "could not read body")
	}

	var info TransferInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return &info, nil
}

// SyncMainDataCtx Sync API implements requests for obtaining changes since the last request.
// Response ID. If not provided, rid=0 will be assumed. If the given rid is different from the one of last server reply, full_update will be true (see the server reply details for more info)
func (c *Client) SyncMainDataCtx(ctx context.Context, rid int64) (*MainData, error) {
	opts := map[string]string{
		"rid": strconv.FormatInt(rid, 10),
	}

	resp, err := c.getCtx(ctx, "/sync/maindata", opts)
	if err != nil {
		return nil, errors.Wrap(err, "could not get main data")
	}

	defer resp.Body.Close()

	var info MainData
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return &info, nil

}

func (c *Client) Pause(hashes []string) error {
	return c.PauseCtx(context.Background(), hashes)
}

func (c *Client) Stop(hashes []string) error {
	return c.PauseCtx(context.Background(), hashes)
}

func (c *Client) StopCtx(ctx context.Context, hashes []string) error {
	return c.PauseCtx(ctx, hashes)
}

func (c *Client) PauseCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
	}

	endpoint := "torrents/stop"

	// Qbt WebAPI 2.11 changed pause with stop
	version, err := c.getApiVersion()
	if err != nil {
		return errors.Wrap(err, "could not get api version")
	}

	if version.Major() == 2 && version.Minor() < 11 {
		endpoint = "torrents/pause"
	}

	resp, err := c.postCtx(ctx, endpoint, opts)
	if err != nil {
		return errors.Wrap(err, "could not pause torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not pause torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) Resume(hashes []string) error {
	return c.ResumeCtx(context.Background(), hashes)
}

func (c *Client) Start(hashes []string) error {
	return c.ResumeCtx(context.Background(), hashes)
}

func (c *Client) StartCtx(ctx context.Context, hashes []string) error {
	return c.ResumeCtx(ctx, hashes)
}

func (c *Client) ResumeCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
	}

	endpoint := "torrents/start"

	// Qbt WebAPI 2.11 changed resume with start
	version, err := c.getApiVersion()

	if err != nil {
		return errors.Wrap(err, "could not get api version")
	}

	if version.Major() == 2 && version.Minor() < 11 {
		endpoint = "torrents/resume"
	}

	resp, err := c.postCtx(ctx, endpoint, opts)
	if err != nil {
		return errors.Wrap(err, "could not resume torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not resume torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) SetForceStart(hashes []string, value bool) error {
	return c.SetForceStartCtx(context.Background(), hashes, value)
}

func (c *Client) SetForceStartCtx(ctx context.Context, hashes []string, value bool) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
		"value":  strconv.FormatBool(value),
	}

	resp, err := c.postCtx(ctx, "torrents/setForceStart", opts)
	if err != nil {
		return errors.Wrap(err, "could not setForceStart torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not setForceStart torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) Recheck(hashes []string) error {
	return c.RecheckCtx(context.Background(), hashes)
}

func (c *Client) RecheckCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.postCtx(ctx, "torrents/recheck", opts)
	if err != nil {
		return errors.Wrap(err, "could not recheck torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not recheck torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) SetAutoManagement(hashes []string, enable bool) error {
	return c.SetAutoManagementCtx(context.Background(), hashes, enable)
}

func (c *Client) SetAutoManagementCtx(ctx context.Context, hashes []string, enable bool) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
		"enable": strconv.FormatBool(enable),
	}

	resp, err := c.postCtx(ctx, "torrents/setAutoManagement", opts)
	if err != nil {
		return errors.Wrap(err, "could not setAutoManagement torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not setAutoManagement torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) SetLocation(hashes []string, location string) error {
	return c.SetLocationCtx(context.Background(), hashes, location)
}

func (c *Client) SetLocationCtx(ctx context.Context, hashes []string, location string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes":   hv,
		"location": location,
	}

	resp, err := c.postCtx(ctx, "torrents/setLocation", opts)
	if err != nil {
		return errors.Wrap(err, "could not setLocation torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not setLocation torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) CreateCategory(category string, path string) error {
	return c.CreateCategoryCtx(context.Background(), category, path)
}

func (c *Client) CreateCategoryCtx(ctx context.Context, category string, path string) error {
	opts := map[string]string{
		"category": category,
		"savePath": path,
	}

	resp, err := c.postCtx(ctx, "torrents/createCategory", opts)
	if err != nil {
		return errors.Wrap(err, "could not createCategory torrents: %v", category)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not createCategory torrents: %v unexpected status: %v", category, resp.StatusCode)
	}

	return nil
}

func (c *Client) EditCategory(category string, path string) error {
	return c.EditCategoryCtx(context.Background(), category, path)
}

func (c *Client) EditCategoryCtx(ctx context.Context, category string, path string) error {
	opts := map[string]string{
		"category": category,
		"savePath": path,
	}

	resp, err := c.postCtx(ctx, "torrents/editCategory", opts)
	if err != nil {
		return errors.Wrap(err, "could not editCategory torrents: %v", category)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not editCategory torrents: %v unexpected status: %v", category, resp.StatusCode)
	}

	return nil
}

func (c *Client) RemoveCategories(categories []string) error {
	return c.RemoveCategoriesCtx(context.Background(), categories)
}

func (c *Client) RemoveCategoriesCtx(ctx context.Context, categories []string) error {
	opts := map[string]string{
		"categories": strings.Join(categories, "\n"),
	}

	resp, err := c.postCtx(ctx, "torrents/removeCategories", opts)
	if err != nil {
		return errors.Wrap(err, "could not removeCategories torrents: %v", opts["categories"])
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not removeCategories torrents: %v unexpected status: %v", opts["categories"], resp.StatusCode)
	}

	return nil
}

func (c *Client) SetCategory(hashes []string, category string) error {
	return c.SetCategoryCtx(context.Background(), hashes, category)
}

func (c *Client) SetCategoryCtx(ctx context.Context, hashes []string, category string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes":   hv,
		"category": category,
	}

	resp, err := c.postCtx(ctx, "torrents/setCategory", opts)
	if err != nil {
		return errors.Wrap(err, "could not setCategory torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not setCategory torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) GetCategories() (map[string]Category, error) {
	return c.GetCategoriesCtx(context.Background())
}

func (c *Client) GetCategoriesCtx(ctx context.Context) (map[string]Category, error) {
	resp, err := c.getCtx(ctx, "torrents/categories", nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not get files info")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "could not read body")
	}

	m := make(map[string]Category)
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return m, nil
}

func (c *Client) GetFilesInformation(hash string) (*TorrentFiles, error) {
	return c.GetFilesInformationCtx(context.Background(), hash)
}

func (c *Client) GetFilesInformationCtx(ctx context.Context, hash string) (*TorrentFiles, error) {
	opts := map[string]string{
		"hash": hash,
	}

	resp, err := c.getCtx(ctx, "torrents/files", opts)
	if err != nil {
		return nil, errors.Wrap(err, "could not get files info")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "could not read body")
	}

	var info TorrentFiles
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return &info, nil
}

// SetFilePriority Set file priority
func (c *Client) SetFilePriority(hash string, IDs string, priority int) error {
	return c.SetFilePriorityCtx(context.Background(), hash, IDs, priority)
}

// SetFilePriorityCtx Set file priority
func (c *Client) SetFilePriorityCtx(ctx context.Context, hash string, IDs string, priority int) error {
	opts := map[string]string{
		"hash":     hash,
		"id":       IDs,
		"priority": strconv.Itoa(priority),
	}

	resp, err := c.postCtx(ctx, "torrents/filePrio", opts)
	if err != nil {
		return errors.Wrap(err, "could not set file priority")
	}

	defer resp.Body.Close()

	/*
		HTTP Status Code 	Scenario
		400		Priority is invalid
		400 	At least one file id is not a valid integer
		404 	Torrent hash was not found
		409 	Torrent metadata hasn't downloaded yet
		409 	At least one file id was not found
		200 	All other scenarios
	*/
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return errors.New("Priority is invalid")
	case http.StatusNotFound:
		return errors.New("torrent %s not found", hash)
	case http.StatusConflict:
		return errors.New("At least one file id was not found")
	case http.StatusOK:
		return nil
	default:
		return errors.New("could not set file priority for torrent: %s unexpected status: %d", hash, resp.StatusCode)
	}
}

func (c *Client) ExportTorrent(hash string) ([]byte, error) {
	return c.ExportTorrentCtx(context.Background(), hash)
}

func (c *Client) ExportTorrentCtx(ctx context.Context, hash string) ([]byte, error) {
	opts := map[string]string{
		"hash": hash,
	}

	resp, err := c.getCtx(ctx, "torrents/export", opts)
	if err != nil {
		return nil, errors.Wrap(err, "could not get export")
	}

	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func (c *Client) RenameFile(hash, oldPath, newPath string) error {
	return c.RenameFileCtx(context.Background(), hash, oldPath, newPath)
}

func (c *Client) RenameFileCtx(ctx context.Context, hash, oldPath, newPath string) error {
	opts := map[string]string{
		"hash":    hash,
		"oldPath": oldPath,
		"newPath": newPath,
	}

	resp, err := c.postCtx(ctx, "torrents/renameFile", opts)
	if err != nil {
		return errors.Wrap(err, "could not renameFile: %v | old: %v | new: %v", hash, oldPath, newPath)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not renameFile: %v | old: %v | new: %v unexpected status: %v", hash, oldPath, newPath, resp.StatusCode)
	}

	return nil
}

// SetTorrentName set name for torrent specified by hash
func (c *Client) SetTorrentName(hash string, name string) error {
	return c.SetTorrentNameCtx(context.Background(), hash, name)
}

// SetTorrentNameCtx set name for torrent specified by hash
func (c *Client) SetTorrentNameCtx(ctx context.Context, hash string, name string) error {
	opts := map[string]string{
		"hash": hash,
		"name": name,
	}

	resp, err := c.postCtx(ctx, "torrents/rename", opts)
	if err != nil {
		return errors.Wrap(err, "could not rename torrent: %v | name: %v", hash, name)
	}

	defer resp.Body.Close()

	switch sc := resp.StatusCode; sc {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return errors.New("torrent hash is invalid: %v", hash)
	case http.StatusConflict:
		return errors.New("torrent name is empty: %v", name)
	default:
		return errors.New("could not rename torrent: %v unexpected status: %v", hash, resp.StatusCode)
	}
}

func (c *Client) GetTags() ([]string, error) {
	return c.GetTagsCtx(context.Background())
}

func (c *Client) GetTagsCtx(ctx context.Context) ([]string, error) {
	resp, err := c.getCtx(ctx, "torrents/tags", nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not get tags")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "could not read body")
	}

	m := make([]string, 0)
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal body")
	}

	return m, nil
}

func (c *Client) CreateTags(tags []string) error {
	return c.CreateTagsCtx(context.Background(), tags)
}

func (c *Client) CreateTagsCtx(ctx context.Context, tags []string) error {
	t := strings.Join(tags, ",")

	opts := map[string]string{
		"tags": t,
	}

	resp, err := c.postCtx(ctx, "torrents/createTags", opts)
	if err != nil {
		return errors.Wrap(err, "could not create tags: %s", t)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not create tags: %s unexpected status: %d", t, resp.StatusCode)
	}

	return nil
}

func (c *Client) AddTags(hashes []string, tags string) error {
	return c.AddTagsCtx(context.Background(), hashes, tags)
}

func (c *Client) AddTagsCtx(ctx context.Context, hashes []string, tags string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
		"tags":   tags,
	}

	resp, err := c.postCtx(ctx, "torrents/addTags", opts)
	if err != nil {
		return errors.Wrap(err, "could not addTags torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not addTags torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

// SetTags is a new method in qBittorrent 5.1 WebAPI 2.11.4 that allows for upserting tags in one go, instead of having to remove and add tags in different calls.
// For client instances with a lot of torrents, this will benefit a lot.
// It checks for the required min version, and if it's less than the required version, it will error, and then the caller can handle it how they want.
func (c *Client) SetTags(ctx context.Context, hashes []string, tags string) error {
	if ok, err := c.RequiresMinVersion(semver.MustParse("2.11.4")); !ok {
		return errors.Wrap(err, "SetTags requires qBittorrent 5.1 and WebAPI >= 2.11.4")
	}

	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
		"tags":   tags,
	}

	resp, err := c.postCtx(ctx, "torrents/setTags", opts)
	if err != nil {
		return errors.Wrap(err, "could not setTags torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not setTags torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

// DeleteTags delete tags from qBittorrent
func (c *Client) DeleteTags(tags []string) error {
	return c.DeleteTagsCtx(context.Background(), tags)
}

// DeleteTagsCtx delete tags from qBittorrent
func (c *Client) DeleteTagsCtx(ctx context.Context, tags []string) error {
	t := strings.Join(tags, ",")

	opts := map[string]string{
		"tags": t,
	}

	resp, err := c.postCtx(ctx, "torrents/deleteTags", opts)
	if err != nil {
		return errors.Wrap(err, "could not delete tags: %s", t)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not delete tags: %s unexpected status: %d", t, resp.StatusCode)
	}

	return nil
}

// RemoveTags remove tags from torrents specified by hashes
func (c *Client) RemoveTags(hashes []string, tags string) error {
	return c.RemoveTagsCtx(context.Background(), hashes, tags)
}

// RemoveTagsCtx remove tags from torrents specified by hashes
func (c *Client) RemoveTagsCtx(ctx context.Context, hashes []string, tags string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")

	opts := map[string]string{
		"hashes": hv,
	}

	if len(tags) != 0 {
		opts["tags"] = tags
	}

	resp, err := c.postCtx(ctx, "torrents/removeTags", opts)
	if err != nil {
		return errors.Wrap(err, "could not removeTags torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not removeTags torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

// EditTracker edit tracker of torrent
func (c *Client) EditTracker(hash string, old, new string) error {
	return c.EditTrackerCtx(context.Background(), hash, old, new)
}

// EditTrackerCtx edit tracker of torrent
func (c *Client) EditTrackerCtx(ctx context.Context, hash string, old, new string) error {
	opts := map[string]string{
		"hash":    hash,
		"origUrl": old,
		"newUrl":  new,
	}

	resp, err := c.postCtx(ctx, "torrents/editTracker", opts)
	if err != nil {
		return errors.Wrap(err, "could not edit tracker for torrent: %s", hash)
	}

	defer resp.Body.Close()

	/*
		HTTP Status Code 	Scenario
		400 	newUrl is not a valid URL
		404 	Torrent hash was not found
		409 	newUrl already exists for the torrent
		409 	origUrl was not found
		200 	All other scenarios
	*/
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return errors.New("new url %s is not a valid URL", new)
	case http.StatusNotFound:
		return errors.New("torrent %s not found", hash)
	case http.StatusConflict:
		return nil
	case http.StatusOK:
		return nil
	default:
		return errors.New("could not edit tracker for torrent: %s unexpected status: %d", hash, resp.StatusCode)
	}
}

// AddTrackers add trackers of torrent
func (c *Client) AddTrackers(hash string, urls string) error {
	return c.AddTrackersCtx(context.Background(), hash, urls)
}

// AddTrackersCtx add trackers of torrent
func (c *Client) AddTrackersCtx(ctx context.Context, hash string, urls string) error {
	opts := map[string]string{
		"hash": hash,
		"urls": urls,
	}

	resp, err := c.postCtx(ctx, "torrents/addTrackers", opts)
	if err != nil {
		return errors.Wrap(err, "could not edit tracker for torrent: %s", hash)
	}

	defer resp.Body.Close()

	/*
		HTTP Status Code 	Scenario
		404 	Torrent hash was not found
		200 	All other scenarios
	*/
	switch resp.StatusCode {
	case http.StatusNotFound:
		return errors.New("torrent %s not found", hash)
	case http.StatusOK:
		return nil
	default:
		return errors.New("could not add trackers for torrent: %s unexpected status: %d", hash, resp.StatusCode)
	}
}

// SetPreferencesQueueingEnabled enable/disable torrent queueing
func (c *Client) SetPreferencesQueueingEnabled(enabled bool) error {
	return c.SetPreferences(map[string]interface{}{"queueing_enabled": enabled})
}

// SetPreferencesMaxActiveDownloads set max active downloads
func (c *Client) SetPreferencesMaxActiveDownloads(max int) error {
	return c.SetPreferences(map[string]interface{}{"max_active_downloads": max})
}

// SetPreferencesMaxActiveTorrents set max active torrents
func (c *Client) SetPreferencesMaxActiveTorrents(max int) error {
	return c.SetPreferences(map[string]interface{}{"max_active_torrents": max})
}

// SetPreferencesMaxActiveUploads set max active uploads
func (c *Client) SetPreferencesMaxActiveUploads(max int) error {
	return c.SetPreferences(map[string]interface{}{"max_active_uploads": max})
}

// SetMaxPriority set torrents to max priority specified by hashes
func (c *Client) SetMaxPriority(hashes []string) error {
	return c.SetMaxPriorityCtx(context.Background(), hashes)
}

// SetMaxPriorityCtx set torrents to max priority specified by hashes
func (c *Client) SetMaxPriorityCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")

	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.postCtx(ctx, "torrents/topPrio", opts)
	if err != nil {
		return errors.Wrap(err, "could not set torrents to maximum priority: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return errors.New("torrent queueing is not enabled, could not set hashes %v to max priority unexpected status: %d", hashes, resp.StatusCode)
	} else if resp.StatusCode != http.StatusOK {
		return errors.New("could not set max priority for torrents: %v unexpected status: %d", hashes, resp.StatusCode)
	}

	return nil
}

// SetMinPriority set torrents to min priority specified by hashes
func (c *Client) SetMinPriority(hashes []string) error {
	return c.SetMinPriorityCtx(context.Background(), hashes)
}

// SetMinPriorityCtx set torrents to min priority specified by hashes
func (c *Client) SetMinPriorityCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")

	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.postCtx(ctx, "torrents/bottomPrio", opts)
	if err != nil {
		return errors.Wrap(err, "could not set torrents to minimum priority: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return errors.New("torrent queueing is not enabled, could not set hashes %v to min priority unexpected status: %d", hashes, resp.StatusCode)
	} else if resp.StatusCode != http.StatusOK {
		return errors.New("could not set min priority for torrents: %v unexpected status: %d", hashes, resp.StatusCode)
	}

	return nil
}

// DecreasePriority decrease priority for torrents specified by hashes
func (c *Client) DecreasePriority(hashes []string) error {
	return c.DecreasePriorityCtx(context.Background(), hashes)
}

// DecreasePriorityCtx decrease priority for torrents specified by hashes
func (c *Client) DecreasePriorityCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")

	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.postCtx(ctx, "torrents/decreasePrio", opts)
	if err != nil {
		return errors.Wrap(err, "could not decrease torrent priority: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return errors.New("torrent queueing is not enabled, could not decrease hashes %v priority unexpected status: %d", hashes, resp.StatusCode)
	} else if resp.StatusCode != http.StatusOK {
		return errors.New("could not decrease priority for torrents: %v unexpected status: %d", hashes, resp.StatusCode)
	}

	return nil
}

// IncreasePriority increase priority for torrents specified by hashes
func (c *Client) IncreasePriority(hashes []string) error {
	return c.IncreasePriorityCtx(context.Background(), hashes)
}

// IncreasePriorityCtx increase priority for torrents specified by hashes
func (c *Client) IncreasePriorityCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")

	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.postCtx(ctx, "torrents/increasePrio", opts)
	if err != nil {
		return errors.Wrap(err, "could not increase torrent priority: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return errors.New("torrent queueing is not enabled, could not increase hashes %v priority unexpected status: %d", hashes, resp.StatusCode)
	} else if resp.StatusCode != http.StatusOK {
		return errors.New("could not increase priority for torrents: %v unexpected status: %d", hashes, resp.StatusCode)
	}

	return nil
}

// ToggleFirstLastPiecePrio toggles the priority of the first and last pieces of torrents specified by hashes
func (c *Client) ToggleFirstLastPiecePrio(hashes []string) error {
	return c.ToggleFirstLastPiecePrioCtx(context.Background(), hashes)
}

// ToggleFirstLastPiecePrioCtx toggles the priority of the first and last pieces of torrents specified by hashes
func (c *Client) ToggleFirstLastPiecePrioCtx(ctx context.Context, hashes []string) error {
	hv := strings.Join(hashes, "|")

	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.postCtx(ctx, "torrents/toggleFirstLastPiecePrio", opts)
	if err != nil {
		return errors.Wrap(err, "could not toggle first/last piece priority for torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("unexpected status while toggling first/last piece priority for torrents: %v, status: %d", hashes, resp.StatusCode)
	}

	return nil
}

// SetTorrentDownloadLimit set download limit for torrents specified by hashes
func (c *Client) SetTorrentDownloadLimit(hashes []string, limit int64) error {
	return c.SetTorrentDownloadLimitCtx(context.Background(), hashes, limit)
}

// SetTorrentDownloadLimitCtx set download limit for torrents specified by hashes
func (c *Client) SetTorrentDownloadLimitCtx(ctx context.Context, hashes []string, limit int64) error {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"limit":  strconv.FormatInt(limit, 10),
	}

	resp, err := c.postCtx(ctx, "torrents/setDownloadLimit", opts)
	if err != nil {
		return errors.Wrap(err, "could not set download limit for torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not set download limit for torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

// SetTorrentShareLimit set share limits for torrents specified by hashes
func (c *Client) SetTorrentShareLimit(hashes []string, ratioLimit float64, seedingTimeLimit int64, inactiveSeedingTimeLimit int64) error {
	return c.SetTorrentShareLimitCtx(context.Background(), hashes, ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit)
}

// SetTorrentShareLimitCtx set share limits for torrents specified by hashes
func (c *Client) SetTorrentShareLimitCtx(ctx context.Context, hashes []string, ratioLimit float64, seedingTimeLimit int64, inactiveSeedingTimeLimit int64) error {
	opts := map[string]string{
		"hashes":                   strings.Join(hashes, "|"),
		"ratioLimit":               strconv.FormatFloat(ratioLimit, 'f', 2, 64),
		"seedingTimeLimit":         strconv.FormatInt(seedingTimeLimit, 10),
		"inactiveSeedingTimeLimit": strconv.FormatInt(inactiveSeedingTimeLimit, 10),
	}

	resp, err := c.postCtx(ctx, "torrents/setShareLimits", opts)
	if err != nil {
		return errors.Wrap(err, "could not set share limits for torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not set share limits for torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

// SetTorrentUploadLimit set upload limit for torrent specified by hashes
func (c *Client) SetTorrentUploadLimit(hashes []string, limit int64) error {
	return c.SetTorrentUploadLimitCtx(context.Background(), hashes, limit)
}

// SetTorrentUploadLimitCtx set upload limit for torrent specified by hashes
func (c *Client) SetTorrentUploadLimitCtx(ctx context.Context, hashes []string, limit int64) error {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"limit":  strconv.FormatInt(limit, 10),
	}

	resp, err := c.postCtx(ctx, "torrents/setUploadLimit", opts)
	if err != nil {
		return errors.Wrap(err, "could not set upload limit for torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("could not set upload limit for torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) GetAppVersion() (string, error) {
	return c.GetAppVersionCtx(context.Background())
}

func (c *Client) GetAppVersionCtx(ctx context.Context) (string, error) {
	resp, err := c.getCtx(ctx, "app/version", nil)
	if err != nil {
		return "", errors.Wrap(err, "could not get app version")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "could not read body")
	}

	return string(body), nil
}

func (c *Client) GetWebAPIVersion() (string, error) {
	return c.GetWebAPIVersionCtx(context.Background())
}

func (c *Client) GetWebAPIVersionCtx(ctx context.Context) (string, error) {
	resp, err := c.getCtx(ctx, "app/webapiVersion", nil)
	if err != nil {
		return "", errors.Wrap(err, "could not get webapi version")
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "could not read body")
	}

	return string(body), nil
}

func (c *Client) GetFreeSpaceOnDisk() (int64, error) {
	return c.GetFreeSpaceOnDiskCtx(context.Background())
}

// GetFreeSpaceOnDiskCtx get free space on disk for default download dir. Expensive call
func (c *Client) GetFreeSpaceOnDiskCtx(ctx context.Context) (int64, error) {
	info, err := c.SyncMainDataCtx(ctx, 0)
	if err != nil {
		return 0, errors.Wrap(err, "could not get maindata")
	}

	return info.ServerState.FreeSpaceOnDisk, nil
}

// RequiresMinVersion checks the current version against version X and errors if the current version is older than X
func (c *Client) RequiresMinVersion(minVersion *semver.Version) (bool, error) {
	version, err := c.getApiVersion()
	if err != nil {
		return false, errors.Wrap(err, "could not get api version")
	}

	if version.LessThan(minVersion) {
		return false, errors.Wrap(ErrUnsupportedVersion, "qBittorrent WebAPI version %s is older than required %s", version.String(), minVersion.String())
	}

	return true, nil
}

const (
	ReannounceMaxAttempts = 50
	ReannounceInterval    = 7 // interval in seconds
)

type ReannounceOptions struct {
	Interval        int
	MaxAttempts     int
	DeleteOnFailure bool
}

func (c *Client) ReannounceTorrentWithRetry(ctx context.Context, hash string, opts *ReannounceOptions) error {
	interval := ReannounceInterval
	maxAttempts := ReannounceMaxAttempts
	deleteOnFailure := false

	if opts != nil {
		if opts.Interval > 0 {
			interval = opts.Interval
		}

		if opts.MaxAttempts > 0 {
			maxAttempts = opts.MaxAttempts
		}

		if opts.DeleteOnFailure {
			deleteOnFailure = opts.DeleteOnFailure
		}
	}

	attempts := 0

	for attempts < maxAttempts {
		c.log.Printf("re-announce %s attempt: %d", hash, attempts)

		// add delay for next run
		time.Sleep(time.Duration(interval) * time.Second)

		trackers, err := c.GetTorrentTrackersCtx(ctx, hash)
		if err != nil {
			return errors.Wrap(err, "could not get trackers for torrent with hash: %s", hash)
		}

		if trackers == nil {
			attempts++
			continue
		}

		c.log.Printf("re-announce %s attempt: %d trackers (%+v)", hash, attempts, trackers)

		// check if status not working or something else
		if isTrackerStatusOK(trackers) {
			c.log.Printf("re-announce for %v OK", hash)

			// if working lets return
			return nil
		}

		c.log.Printf("not working yet, lets re-announce %s attempt: %d", hash, attempts)

		if err = c.ReAnnounceTorrentsCtx(ctx, []string{hash}); err != nil {
			return errors.Wrap(err, "could not re-announce torrent with hash: %s", hash)
		}

		attempts++
	}

	// delete on failure to reannounce
	if deleteOnFailure {
		c.log.Printf("re-announce for %s took too long, deleting torrent", hash)

		if err := c.DeleteTorrentsCtx(ctx, []string{hash}, false); err != nil {
			return errors.Wrap(err, "could not delete torrent with hash: %s", hash)
		}

		return ErrReannounceTookTooLong
	}

	return nil
}

// Check if status not working or something else
// https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-4.1)#get-torrent-trackers
//
//	0 Tracker is disabled (used for DHT, PeX, and LSD)
//	1 Tracker has not been contacted yet
//	2 Tracker has been contacted and is working
//	3 Tracker is updating
//	4 Tracker has been contacted, but it is not working (or doesn't send proper replies)
func isTrackerStatusOK(trackers []TorrentTracker) bool {
	for _, tracker := range trackers {
		if tracker.Status == TrackerStatusDisabled {
			continue
		}

		// check for certain messages before the tracker status to catch ok status with unreg msg
		if isUnregistered(tracker.Message) {
			return false
		}

		if tracker.Status == TrackerStatusOK {
			return true
		}
	}

	return false
}

func isUnregistered(msg string) bool {
	words := []string{"unregistered", "not registered", "not found", "not exist"}

	msg = strings.ToLower(msg)

	for _, v := range words {
		if strings.Contains(msg, v) {
			return true
		}
	}

	return false
}
