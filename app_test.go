package qbittorrent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestClient_GetNetworkInterfacesCtx(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/app/networkInterfaceList", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"Ethernet","value":"eth0"},{"name":"WireGuard","value":"wg0"}]`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(Config{Host: server.URL})

	got, err := client.GetNetworkInterfacesCtx(t.Context())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []NetworkInterface{{Name: "Ethernet", Value: "eth0"}, {Name: "WireGuard", Value: "wg0"}}
	if !slices.Equal(got, want) {
		t.Fatalf("interfaces = %#v, want %#v", got, want)
	}
}

func TestClient_GetNetworkInterfaceAddressesCtx(t *testing.T) {
	var gotIface string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/app/networkInterfaceAddressList", func(w http.ResponseWriter, r *http.Request) {
		gotIface = r.URL.Query().Get("iface")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["10.0.0.2","fd00::2"]`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(Config{Host: server.URL})

	got, err := client.GetNetworkInterfaceAddressesCtx(t.Context(), "wg0")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"10.0.0.2", "fd00::2"}; !slices.Equal(got, want) {
		t.Fatalf("addresses = %#v, want %#v", got, want)
	}
	if gotIface != "wg0" {
		t.Fatalf("iface = %q, want wg0", gotIface)
	}
}

func TestClient_SendTestEmailCtx(t *testing.T) {
	tests := []struct {
		version string
		wantErr error
	}{
		{version: "2.9.3", wantErr: ErrUnsupportedVersion},
		{version: "2.11.2"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			sent := false
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.version))
			})
			mux.HandleFunc("POST /api/v2/app/sendTestEmail", func(_ http.ResponseWriter, _ *http.Request) {
				sent = true
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			client := NewClient(Config{Host: server.URL})

			err := client.SendTestEmailCtx(t.Context())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if wantSent := tt.wantErr == nil; sent != wantSent {
				t.Fatalf("sent = %v, want %v", sent, wantSent)
			}
		})
	}
}
