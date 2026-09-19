package qbittorrent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A server older than WebAPI 2.11.8 ignores withMetadata and answers with the
// plain string list, so the client must refuse before it decodes that.
func TestClient_ListDirectory_MetadataVersionGate(t *testing.T) {
	var gotMode string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2.11.2"))
	})
	mux.HandleFunc("/api/v2/app/getDirectoryContent", func(w http.ResponseWriter, r *http.Request) {
		gotMode = r.URL.Query().Get("mode")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["/data/sub"]`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(Config{Host: server.URL})

	_, err := client.ListDirectory("/data", DirectoryContentAll, true)
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("withMetadata on 2.11.2: err = %v, want ErrUnsupportedVersion", err)
	}

	entries, err := client.ListDirectory("/data", DirectoryContentAll, false)
	if err != nil {
		t.Fatalf("without metadata on 2.11.2: err = %v", err)
	}
	got, ok := entries.([]string)
	if !ok || len(got) != 1 || got[0] != "/data/sub" {
		t.Fatalf("entries = %#v, want []string{\"/data/sub\"}", entries)
	}
	if gotMode != "all" {
		t.Fatalf("mode = %q, want all", gotMode)
	}
}
