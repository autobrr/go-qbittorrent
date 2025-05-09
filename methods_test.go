//go:build !ci
// +build !ci

package qbittorrent_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/go-qbittorrent"
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
	qBittorrentPassword = "admin"
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
