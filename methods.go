package qbittorrent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"github.com/autobrr/go-qbittorrent/errors"
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
	} else {
		return errors.New("bad credentials")
	}

	c.log.Printf("logged into client: %v", c.cfg.Host)

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
		return errors.Wrap(err, "could not add torrent %v unexpected status: %v", filePath, res.StatusCode)
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
		return errors.Wrap(err, "could not add torrent %v unexpected status: %v", url, res.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/delete", opts)
	if err != nil {
		return errors.Wrap(err, "could not delete torrents: %+v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not delete torrents %v unexpected status: %v", hashes, resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/reannounce", opts)
	if err != nil {
		return errors.Wrap(err, "could not re-announce torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not re-announce torrents: %v unexpected status: %v", hashes, resp.StatusCode)
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

func (c *Client) Pause(hashes []string) error {
	return c.PauseCtx(context.Background(), hashes)
}

func (c *Client) PauseCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.getCtx(ctx, "torrents/pause", opts)
	if err != nil {
		return errors.Wrap(err, "could not pause torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not pause torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
}

func (c *Client) Resume(hashes []string) error {
	return c.ResumeCtx(context.Background(), hashes)
}

func (c *Client) ResumeCtx(ctx context.Context, hashes []string) error {
	// Add hashes together with | separator
	hv := strings.Join(hashes, "|")
	opts := map[string]string{
		"hashes": hv,
	}

	resp, err := c.getCtx(ctx, "torrents/resume", opts)
	if err != nil {
		return errors.Wrap(err, "could not resume torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not resume torrents: %v unexpected status: %v", hashes, resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/setForceStart", opts)
	if err != nil {
		return errors.Wrap(err, "could not setForceStart torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not setForceStart torrents: %v unexpected status: %v", hashes, resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/recheck", opts)
	if err != nil {
		return errors.Wrap(err, "could not recheck torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not recheck torrents: %v unexpected status: %v", hashes, resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/setAutoManagement", opts)
	if err != nil {
		return errors.Wrap(err, "could not setAutoManagement torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not setAutoManagement torrents: %v unexpected status: %v", hashes, resp.StatusCode)
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
		return errors.Wrap(err, "could not setLocation torrents: %v unexpected status: %v", hashes, resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/createCategory", opts)
	if err != nil {
		return errors.Wrap(err, "could not createCategory torrents: %v", category)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not createCategory torrents: %v unexpected status: %v", category, resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/editCategory", opts)
	if err != nil {
		return errors.Wrap(err, "could not editCategory torrents: %v", category)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not editCategory torrents: %v unexpected status: %v", category, resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/removeCategories", opts)
	if err != nil {
		return errors.Wrap(err, "could not removeCategories torrents: %v", opts["categories"])
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not removeCategories torrents: %v unexpected status: %v", opts["categories"], resp.StatusCode)
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

	resp, err := c.getCtx(ctx, "torrents/setCategory", opts)
	if err != nil {
		return errors.Wrap(err, "could not setCategory torrents: %v", hashes)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrap(err, "could not setCategory torrents: %v unexpected status: %v", hashes, resp.StatusCode)
	}

	return nil
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
		return errors.Wrap(err, "could not renameFile: %v | old: %v | new: %v unexpected status: %v", hash, oldPath, newPath, resp.StatusCode)
	}

	return nil
}
