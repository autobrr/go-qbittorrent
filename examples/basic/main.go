package main

import (
	"context"
	"log"

	"github.com/autobrr/go-qbittorrent"
)

func main() {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     "http://localhost:8080",
		Username: "admin",
		Password: "adminadmin",
	})

	ctx := context.Background()

	if err := client.LoginCtx(ctx); err != nil {
		log.Fatalf("could not log into client: %q", err)
	}

	torrents, err := client.GetTorrents(qbittorrent.TorrentFilterOptions{
		Category: "test",
	})
	if err != nil {
		log.Fatalf("could not get torrents from client: %q", err)
	}

	log.Printf("Found %d torrents", len(torrents))
}
