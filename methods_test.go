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
