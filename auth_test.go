package qbittorrent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type authLoginCookieJar struct{}

func (authLoginCookieJar) SetCookies(*url.URL, []*http.Cookie) {}

func (authLoginCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if u.Path == "/api/v2/auth/login" {
		return nil
	}

	panic("LoginCtx checked cookies before posting auth/login")
}

type postBasicCookieJar struct{}

func (postBasicCookieJar) SetCookies(*url.URL, []*http.Cookie) {}

func (postBasicCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if u.Path == "/api/v2/transfer/info" {
		return nil
	}

	panic("postBasicCtx checked session cookies before posting")
}

func TestClient_LoginDoesNotRequireExistingCookie(t *testing.T) {
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		if r.URL.Path != "/api/v2/auth/login" {
			t.Fatalf("path = %q, want /api/v2/auth/login", r.URL.Path)
		}

		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-session"})
		_, _ = w.Write([]byte("Ok."))
	}))
	defer server.Close()

	client := NewClient(Config{
		Host:     server.URL,
		Username: "admin",
		Password: "password",
	})
	client.http.Jar = authLoginCookieJar{}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("LoginCtx() panicked: %v", recovered)
		}
	}()

	if err := client.LoginCtx(context.Background()); err != nil {
		t.Fatalf("LoginCtx() error = %v", err)
	}

	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestClient_PostBasicDoesNotCheckCookies(t *testing.T) {
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		if r.URL.Path != "/api/v2/transfer/info" {
			t.Fatalf("path = %q, want /api/v2/transfer/info", r.URL.Path)
		}

		_, _ = w.Write([]byte("Ok."))
	}))
	defer server.Close()

	client := NewClient(Config{
		Host:     server.URL,
		Username: "admin",
		Password: "password",
	})
	client.http.Jar = postBasicCookieJar{}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("postBasicCtx() checked cookies: %v", recovered)
		}
	}()

	resp, err := client.postBasicCtx(context.Background(), "transfer/info", nil)
	if err != nil {
		t.Fatalf("postBasicCtx() error = %v", err)
	}
	defer drainAndClose(resp)

	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
