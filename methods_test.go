//go:build !ci
// +build !ci

package qbittorrent_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/go-qbittorrent"
)

const (
	// a sample torrent that only contains one folder "untitled" and one file "untitled.txt".
	sampleTorrent  = "d10:created by18:qBittorrent v5.1.013:creation datei1747004328e4:infod5:filesld6:lengthi21e4:pathl12:untitled.txteee4:name8:untitled12:piece lengthi16384e6:pieces20:\xb5|\x901\xce\xa3\xdb @$\xce\xbd\xd3\xb0\x0e\xd3\xba\xc0\xcc\xbd7:privatei1eee"
	sampleInfoHash = "ead9241e611e9712f28b20b151f1a3ecd4a6178a"
)

var (
	qBittorrentBaseURL  string
	qBittorrentUsername string
	qBittorrentPassword string
)

func init() {
	qBittorrentBaseURL = "http://127.0.0.1:8080/"
	if val := os.Getenv("QBIT_BASE_URL"); val != "" {
		qBittorrentBaseURL = val
	}
	qBittorrentUsername = "admin"
	if val := os.Getenv("QBIT_USERNAME"); val != "" {
		qBittorrentUsername = val
	}
	qBittorrentPassword = "password" // must be at least 6 characters
	if val := os.Getenv("QBIT_PASSWORD"); val != "" {
		qBittorrentPassword = val
	}
}

func TestClient_GetDefaultSavePath(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	_, err := client.GetDefaultSavePath()
	assert.NoError(t, err)
}

func TestClient_GetAppCookies(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	_, err := client.GetAppCookies()
	assert.NoError(t, err)
}

func TestClient_SetAppCookies(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	var err error
	var cookies = []qbittorrent.Cookie{
		{
			Name:           "test",
			Domain:         "example.com",
			Path:           "/",
			Value:          "test",
			ExpirationDate: time.Now().Add(time.Hour).Unix(),
		},
	}
	err = client.SetAppCookies(cookies)
	assert.NoError(t, err)

	resp, err := client.GetAppCookies()
	assert.NoError(t, err)
	assert.NotEmpty(t, cookies)
	assert.Equal(t, cookies, resp)
}

func TestClient_BanPeers(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	err := client.BanPeers([]string{"127.0.0.1:80"})
	assert.NoError(t, err)
}

func TestClient_GetBuildInfo(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	bi, err := client.GetBuildInfo()
	assert.NoError(t, err)
	assert.NotEmpty(t, bi.Qt)
	assert.NotEmpty(t, bi.Libtorrent)
	assert.NotEmpty(t, bi.Boost)
	assert.NotEmpty(t, bi.Openssl)
	assert.NotEmpty(t, bi.Bitness)
}

func TestClient_GetTorrentDownloadLimit(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	var hashes []string
	for _, torrent := range data {
		hashes = append(hashes, torrent.Hash)
	}

	limits, err := client.GetTorrentDownloadLimit(hashes)
	assert.NoError(t, err)
	assert.Equal(t, len(hashes), len(limits))

	// FIXME: The following assertion will fail.
	// Neither "hashes=all" nor "all" is working.
	// I have no idea. Maybe the document is lying?
	//
	// limits, err = client.GetTorrentDownloadLimit([]string{"all"})
	// assert.NoError(t, err)
	// assert.Equal(t, len(hashes), len(limits))
}

func TestClient_GetTorrentUploadLimit(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	var hashes []string
	for _, torrent := range data {
		hashes = append(hashes, torrent.Hash)
	}

	limits, err := client.GetTorrentUploadLimit(hashes)
	assert.NoError(t, err)
	assert.Equal(t, len(hashes), len(limits))

	// FIXME: The following assertion will fail.
	// Neither "hashes=all" nor "all" is working.
	// I have no idea. Maybe the document is lying?
	// Just as same as Client.GetTorrentDownloadLimit.
	//
	// limits, err = client.GetTorrentDownloadLimit([]string{"all"})
	// assert.NoError(t, err)
	// assert.Equal(t, len(hashes), len(limits))
}

func TestClient_ToggleTorrentSequentialDownload(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	var err error

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	var hashes []string
	for _, torrent := range data {
		hashes = append(hashes, torrent.Hash)
	}

	err = client.ToggleTorrentSequentialDownload(hashes)
	assert.NoError(t, err)

	// No idea why this is working but downloadLimit/uploadLimit are not.
	err = client.ToggleTorrentSequentialDownload([]string{"all"})
	assert.NoError(t, err)
}

func TestClient_SetTorrentSuperSeeding(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	var err error

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	var hashes []string
	for _, torrent := range data {
		hashes = append(hashes, torrent.Hash)
	}

	err = client.SetTorrentSuperSeeding(hashes, true)
	assert.NoError(t, err)

	// FIXME: following test not fail but has no effect.
	// qBittorrent doesn't return any error but super seeding status is not changed.
	// I tried specify hashes as "all" but it's not working too.
	err = client.SetTorrentSuperSeeding([]string{"all"}, false)
	assert.NoError(t, err)
}

func TestClient_GetTorrentPieceStates(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	if len(data) == 0 {
		t.Skip("No torrents available for testing")
	}

	hash := data[0].Hash
	states, err := client.GetTorrentPieceStates(hash)
	assert.NoError(t, err)
	assert.NotEmpty(t, states)
}

func TestClient_GetTorrentPieceHashes(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	if len(data) == 0 {
		t.Skip("No torrents available for testing")
	}

	hash := data[0].Hash
	states, err := client.GetTorrentPieceHashes(hash)
	assert.NoError(t, err)
	assert.NotEmpty(t, states)
}

func TestClient_AddPeersForTorrents(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	hashes := []string{data[0].Hash}
	peers := []string{"127.0.0.1:12345"}
	err = client.AddPeersForTorrents(hashes, peers)
	// It seems qBittorrent doesn't actually check whether given peers are available.
	assert.NoError(t, err)
}

func TestClient_RenameFile(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	err := client.AddTorrentFromMemory([]byte(sampleTorrent), nil)
	assert.NoError(t, err)
	defer func(client *qbittorrent.Client) {
		_ = client.DeleteTorrents([]string{sampleInfoHash}, false)
	}(client)

	err = client.RenameFile(sampleInfoHash, "untitled/untitled.txt", "untitled/renamed.txt")
	assert.NoError(t, err)
}

func TestClient_RenameFolder(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	err := client.AddTorrentFromMemory([]byte(sampleTorrent), nil)
	assert.NoError(t, err)
	defer func(client *qbittorrent.Client) {
		_ = client.DeleteTorrents([]string{sampleInfoHash}, false)
	}(client)

	err = client.RenameFolder(sampleInfoHash, "untitled", "renamed")
	assert.NoError(t, err)
}

func TestClient_GetTorrentsWebSeeds(t *testing.T) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     qBittorrentBaseURL,
		Username: qBittorrentUsername,
		Password: qBittorrentPassword,
	})

	data, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{})
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	hash := data[0].Hash
	_, err = client.GetTorrentsWebSeeds(hash)
	assert.NoError(t, err)
}
