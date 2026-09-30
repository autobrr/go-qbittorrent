package qbittorrent

import (
	"strings"
	"testing"
)

func TestDecodeMainData(t *testing.T) {
	body := strings.NewReader(`{"rid":1,"torrents":{"abc":{"name":"test"}}}`)

	data, raw, err := decodeMainData(body)
	if err != nil {
		t.Fatalf("decode main data: %v", err)
	}

	if got := data.Torrents["abc"].Hash; got != "abc" {
		t.Errorf("typed torrent hash = %q, want %q", got, "abc")
	}

	torrents, ok := raw["torrents"].(map[string]interface{})
	if !ok {
		t.Fatalf("raw torrents has type %T", raw["torrents"])
	}
	torrent, ok := torrents["abc"].(map[string]interface{})
	if !ok {
		t.Fatalf("raw torrent has type %T", torrents["abc"])
	}
	if _, exists := torrent["hash"]; exists {
		t.Error("raw torrent hash should not be synthesized")
	}
}

func TestDecodeMainDataError(t *testing.T) {
	data, raw, err := decodeMainData(strings.NewReader(`{"rid":"invalid"}`))
	if err == nil {
		t.Fatal("expected decode error")
	}
	if data != nil || raw != nil {
		t.Fatalf("decode error returned data=%v raw=%v", data, raw)
	}
}

func TestMergeTorrentsPartialUsesMapKeyAsHash(t *testing.T) {
	dest := MainData{
		Torrents: map[string]Torrent{
			"abc": {Hash: "stale", Name: "old"},
		},
	}

	dest.mergeTorrentsPartial(map[string]interface{}{
		"abc": map[string]interface{}{"name": "new"},
	})

	got := dest.Torrents["abc"]
	if got.Hash != "abc" {
		t.Errorf("torrent hash = %q, want %q", got.Hash, "abc")
	}
	if got.Name != "new" {
		t.Errorf("torrent name = %q, want %q", got.Name, "new")
	}
}
