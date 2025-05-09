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
