package qbittorrent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetProcessInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/app/processInfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"launch_time":1769331513}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(Config{
		Host: server.URL,
	})

	info, err := client.GetProcessInfo()
	if err != nil {
		t.Fatalf("GetProcessInfo() error = %v", err)
	}

	if info.LaunchTime != 1769331513 {
		t.Fatalf("LaunchTime = %d, want 1769331513", info.LaunchTime)
	}
}
