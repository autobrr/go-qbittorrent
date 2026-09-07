package qbittorrent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestClient_GetFreeSpaceAtPathCtx(t *testing.T) {
	const path = "/remote/downloads/new folder/日本語 + & # ? %/child"

	for _, tt := range []struct {
		name    string
		body    string
		status  int
		want    int64
		wantErr error
	}{
		{"positive", "1099511627776", http.StatusOK, 1099511627776, nil},
		{"zero", "0", http.StatusOK, 0, nil},
		{"unavailable", "-1", http.StatusOK, -1, nil},
		{"negative", "-42", http.StatusOK, -42, nil},
		{"max int64", "9223372036854775807", http.StatusOK, 9223372036854775807, nil},
		{"min int64", "-9223372036854775808", http.StatusOK, -9223372036854775808, nil},
		{"empty", "", http.StatusOK, 0, strconv.ErrSyntax},
		{"invalid", "unavailable", http.StatusOK, 0, strconv.ErrSyntax},
		{"fraction", "1.5", http.StatusOK, 0, strconv.ErrSyntax},
		{"trailing data", "123\n456", http.StatusOK, 0, strconv.ErrSyntax},
		{"overflow", "9223372036854775808", http.StatusOK, 0, strconv.ErrRange},
		{"underflow", "-9223372036854775809", http.StatusOK, 0, strconv.ErrRange},
		{"unsupported server", "Not Found", http.StatusNotFound, 0, ErrUnexpectedStatus},
		{"bad request", "Bad Request", http.StatusBadRequest, 0, ErrUnexpectedStatus},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v2/app/getFreeSpaceAtPath", func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if len(query) != 1 || len(query["path"]) != 1 || query.Get("path") != path {
					t.Errorf("query = %v, want only path=%q", query, path)
				}
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := NewClient(Config{Host: server.URL, RetryAttempts: 1})

			got, err := client.GetFreeSpaceAtPathCtx(t.Context(), path)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetFreeSpaceAtPathCtx() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("GetFreeSpaceAtPathCtx() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClient_GetFreeSpaceAtPathCtx_Canceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("canceled request reached the server")
	}))
	defer server.Close()
	client := NewClient(Config{Host: server.URL, RetryAttempts: 1})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := client.GetFreeSpaceAtPathCtx(ctx, "/remote/downloads"); err == nil {
		t.Fatal("GetFreeSpaceAtPathCtx() returned no error for a canceled context")
	}
}
