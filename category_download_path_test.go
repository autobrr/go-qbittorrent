package qbittorrent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func newCategoryTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	return newCategoryTestClientVersion(t, "2.11.2", handler)
}

func newCategoryTestClientVersion(t *testing.T, version string, handler http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(version))
	})
	mux.HandleFunc("/", handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return NewClient(Config{Host: server.URL, APIKey: "fixture"})
}

func TestGetCategoriesDownloadPath(t *testing.T) {
	// Each category is one way qBittorrent writes download_path. 5.1 leaves the
	// key out for inherit and 5.2 writes null.
	body := `{
		"absent":   {"name": "absent", "savePath": "/data/absent"},
		"null":     {"name": "null", "savePath": "/data/null", "download_path": null},
		"disabled": {"name": "disabled", "savePath": "/data/disabled", "download_path": false},
		"true":     {"name": "true", "savePath": "/data/true", "download_path": true},
		"empty":    {"name": "empty", "savePath": "/data/empty", "download_path": ""},
		"absolute": {"name": "absolute", "savePath": "/data/absolute", "download_path": "/incomplete/absolute"},
		"relative": {"name": "relative", "savePath": "/data/relative", "download_path": "relative/sub"}
	}`
	client := newCategoryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/torrents/categories", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})

	categories, err := client.GetCategoriesCtx(context.Background())
	require.NoError(t, err)

	want := map[string]*CategoryDownloadPath{
		"absent":   nil,
		"null":     nil,
		"disabled": {Enabled: false},
		"true":     {Enabled: true},
		"empty":    {Enabled: true},
		"absolute": {Enabled: true, Path: "/incomplete/absolute"},
		"relative": {Enabled: true, Path: "relative/sub"},
	}
	require.Len(t, categories, len(want))
	for name, downloadPath := range want {
		require.Contains(t, categories, name)
		require.Equal(t, downloadPath, categories[name].DownloadPath, "category %s", name)
		require.Equal(t, "/data/"+name, categories[name].SavePath, "category %s", name)
	}
}

func TestCategoryDownloadPathRejectsOtherJSONTypes(t *testing.T) {
	for _, raw := range []string{`1`, `{}`, `[]`} {
		var category Category
		err := json.Unmarshal([]byte(`{"name":"x","download_path":`+raw+`}`), &category)
		require.Error(t, err, "download_path %s", raw)
	}
}

func TestCategoryDownloadPathMarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		category Category
		json     string
	}{
		{"inherit", Category{Name: "a", SavePath: "/s"}, `{"name":"a","savePath":"/s"}`},
		{"disabled", Category{Name: "a", SavePath: "/s", DownloadPath: &CategoryDownloadPath{}}, `{"name":"a","savePath":"/s","download_path":false}`},
		{"enabled empty", Category{Name: "a", SavePath: "/s", DownloadPath: &CategoryDownloadPath{Enabled: true}}, `{"name":"a","savePath":"/s","download_path":""}`},
		{"enabled", Category{Name: "a", SavePath: "/s", DownloadPath: &CategoryDownloadPath{Enabled: true, Path: "/dl"}}, `{"name":"a","savePath":"/s","download_path":"/dl"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.category)
			require.NoError(t, err)
			require.JSONEq(t, tt.json, string(data))

			var decoded Category
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Equal(t, tt.category, decoded)
		})
	}

	// A disabled option drops its path, as qBittorrent's own toJSON does.
	data, err := json.Marshal(CategoryDownloadPath{Enabled: false, Path: "/ignored"})
	require.NoError(t, err)
	require.Equal(t, `false`, string(data))
}

func TestMainDataCategoryDownloadPathUpdates(t *testing.T) {
	// Each response follows the previous one. qBittorrent sends only the keys
	// that changed in a partial update, and null when download_path goes back
	// to inherit.
	responses := []string{
		`{"rid":1,"full_update":true,"categories":{
			"movies": {"name":"movies","savePath":"/m","download_path":"/dl/movies"},
			"tv":     {"name":"tv","savePath":"/t","download_path":false},
			"music":  {"name":"music","savePath":"/a"}
		}}`,
		// string to null, false to string, and a new category.
		`{"rid":2,"categories":{
			"movies": {"download_path":null},
			"tv":     {"download_path":"/dl/tv"},
			"books":  {"name":"books","savePath":"/b","download_path":true}
		}}`,
		// absent key leaves the value alone.
		`{"rid":3,"categories":{
			"tv":     {"savePath":"/t2"}
		}}`,
		// string to false, null to string.
		`{"rid":4,"categories":{
			"tv":     {"download_path":false},
			"movies": {"download_path":"relative"}
		}}`,
	}
	step := 0
	client := newCategoryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/sync/maindata", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[step]))
		step++
	})

	ctx := context.Background()
	data := &MainData{}

	require.NoError(t, data.Update(ctx, client))
	require.Equal(t, &CategoryDownloadPath{Enabled: true, Path: "/dl/movies"}, data.Categories["movies"].DownloadPath)
	require.Equal(t, &CategoryDownloadPath{Enabled: false}, data.Categories["tv"].DownloadPath)
	require.Nil(t, data.Categories["music"].DownloadPath)

	require.NoError(t, data.Update(ctx, client))
	require.Nil(t, data.Categories["movies"].DownloadPath, "string to null")
	require.Equal(t, "/m", data.Categories["movies"].SavePath)
	require.Equal(t, &CategoryDownloadPath{Enabled: true, Path: "/dl/tv"}, data.Categories["tv"].DownloadPath, "false to string")
	require.Equal(t, Category{Name: "books", SavePath: "/b", DownloadPath: &CategoryDownloadPath{Enabled: true}}, data.Categories["books"])
	require.Nil(t, data.Categories["music"].DownloadPath)

	require.NoError(t, data.Update(ctx, client))
	require.Equal(t, "/t2", data.Categories["tv"].SavePath)
	require.Equal(t, &CategoryDownloadPath{Enabled: true, Path: "/dl/tv"}, data.Categories["tv"].DownloadPath, "absent key")

	require.NoError(t, data.Update(ctx, client))
	require.Equal(t, &CategoryDownloadPath{Enabled: false}, data.Categories["tv"].DownloadPath, "string to false")
	require.Equal(t, &CategoryDownloadPath{Enabled: true, Path: "relative"}, data.Categories["movies"].DownloadPath, "null to string")
}

func TestMainDataCategoryDownloadPathPartialUpdateDoesNotAlias(t *testing.T) {
	// The updater must store a fresh pointer, so a later update to one
	// category never changes a copy taken earlier.
	data := &MainData{}
	data.UpdateWithRawData(map[string]interface{}{
		"categories": map[string]interface{}{
			"a": map[string]interface{}{"name": "a", "download_path": "/one"},
		},
	}, &MainData{Rid: 1})
	before := data.Categories["a"].DownloadPath
	require.Equal(t, &CategoryDownloadPath{Enabled: true, Path: "/one"}, before)

	data.UpdateWithRawData(map[string]interface{}{
		"categories": map[string]interface{}{
			"a": map[string]interface{}{"download_path": "/two"},
		},
	}, &MainData{Rid: 2})
	require.Equal(t, "/one", before.Path)
	require.Equal(t, "/two", data.Categories["a"].DownloadPath.Path)
}

func captureCategoryForm(t *testing.T, endpoint string) (*Client, *url.Values) {
	t.Helper()
	var form url.Values
	client := newCategoryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/torrents/"+endpoint, r.URL.Path)
		require.NoError(t, r.ParseForm())
		form = r.PostForm
		w.WriteHeader(http.StatusOK)
	})
	return client, &form
}

func TestCreateCategoryWithOptionsForm(t *testing.T) {
	tests := []struct {
		name string
		opts CategoryOptions
		want url.Values
	}{
		{"inherit", CategoryOptions{SavePath: "/s"}, url.Values{"category": {"c"}, "savePath": {"/s"}}},
		{"disabled", CategoryOptions{SavePath: "/s", DownloadPath: &CategoryDownloadPath{}},
			url.Values{"category": {"c"}, "savePath": {"/s"}, "downloadPathEnabled": {"false"}, "downloadPath": {""}}},
		{"enabled", CategoryOptions{SavePath: "/s", DownloadPath: &CategoryDownloadPath{Enabled: true, Path: "/dl"}},
			url.Values{"category": {"c"}, "savePath": {"/s"}, "downloadPathEnabled": {"true"}, "downloadPath": {"/dl"}}},
		{"enabled empty", CategoryOptions{DownloadPath: &CategoryDownloadPath{Enabled: true}},
			url.Values{"category": {"c"}, "savePath": {""}, "downloadPathEnabled": {"true"}, "downloadPath": {""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, form := captureCategoryForm(t, "createCategory")
			require.NoError(t, client.CreateCategoryWithOptionsCtx(context.Background(), "c", tt.opts))
			require.Equal(t, tt.want, *form)
		})
	}
}

func TestEditCategoryWithOptionsForm(t *testing.T) {
	// Every mode states the download path, so it ends up the same on
	// qBittorrent up to 5.2, which replaces every option, and from 5.3.0,
	// which keeps an option the form leaves out. An empty downloadPathEnabled
	// means inherit on both.
	tests := []struct {
		name string
		opts CategoryOptions
		want url.Values
	}{
		{"inherit", CategoryOptions{SavePath: "/s"},
			url.Values{"category": {"c"}, "savePath": {"/s"}, "downloadPathEnabled": {""}}},
		{"disabled", CategoryOptions{SavePath: "/s", DownloadPath: &CategoryDownloadPath{Path: "/ignored"}},
			url.Values{"category": {"c"}, "savePath": {"/s"}, "downloadPathEnabled": {"false"}, "downloadPath": {"/ignored"}}},
		{"enabled", CategoryOptions{SavePath: "/s", DownloadPath: &CategoryDownloadPath{Enabled: true, Path: "/dl"}},
			url.Values{"category": {"c"}, "savePath": {"/s"}, "downloadPathEnabled": {"true"}, "downloadPath": {"/dl"}}},
		{"empty save path", CategoryOptions{},
			url.Values{"category": {"c"}, "savePath": {""}, "downloadPathEnabled": {""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, form := captureCategoryForm(t, "editCategory")
			require.NoError(t, client.EditCategoryWithOptionsCtx(context.Background(), "c", tt.opts))
			require.Equal(t, tt.want, *form)
		})
	}
}

func TestCreateAndEditCategoryFormUnchanged(t *testing.T) {
	// The old methods must keep sending exactly category and savePath.
	want := url.Values{"category": {"c"}, "savePath": {"/s"}}

	client, form := captureCategoryForm(t, "createCategory")
	require.NoError(t, client.CreateCategoryCtx(context.Background(), "c", "/s"))
	require.Equal(t, want, *form)

	client, form = captureCategoryForm(t, "editCategory")
	require.NoError(t, client.EditCategoryCtx(context.Background(), "c", "/s"))
	require.Equal(t, want, *form)
}

func TestCategoryWithOptionsStatusCodes(t *testing.T) {
	statusClient := func(status int) *Client {
		return newCategoryTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		})
	}
	ctx := context.Background()

	require.ErrorIs(t, statusClient(http.StatusBadRequest).CreateCategoryWithOptionsCtx(ctx, "", CategoryOptions{}), ErrEmptyCategoryName)
	require.ErrorIs(t, statusClient(http.StatusConflict).CreateCategoryWithOptionsCtx(ctx, "c", CategoryOptions{}), ErrInvalidCategoryName)
	require.ErrorIs(t, statusClient(http.StatusBadRequest).EditCategoryWithOptionsCtx(ctx, "", CategoryOptions{}), ErrEmptyCategoryName)
	require.ErrorIs(t, statusClient(http.StatusConflict).EditCategoryWithOptionsCtx(ctx, "c", CategoryOptions{}), ErrCategoryEditingFailed)
	require.ErrorIs(t, statusClient(http.StatusTeapot).EditCategoryWithOptionsCtx(ctx, "c", CategoryOptions{}), ErrUnexpectedStatus)
}

func TestCategoryWithOptionsVersionGate(t *testing.T) {
	// WebAPI 2.8.3 ignores the download path form fields, so a call that sets
	// one must fail instead of reporting success.
	posted := 0
	client := newCategoryTestClientVersion(t, "2.8.3", func(w http.ResponseWriter, _ *http.Request) {
		posted++
		w.WriteHeader(http.StatusOK)
	})
	ctx := context.Background()
	set := CategoryOptions{SavePath: "/s", DownloadPath: &CategoryDownloadPath{Enabled: true, Path: "/dl"}}

	require.ErrorIs(t, client.CreateCategoryWithOptionsCtx(ctx, "c", set), ErrUnsupportedVersion)
	require.ErrorIs(t, client.EditCategoryWithOptionsCtx(ctx, "c", set), ErrUnsupportedVersion)
	require.Zero(t, posted)

	// Inherit needs no version: older servers read the empty field as unset.
	require.NoError(t, client.CreateCategoryWithOptionsCtx(ctx, "c", CategoryOptions{SavePath: "/s"}))
	require.NoError(t, client.EditCategoryWithOptionsCtx(ctx, "c", CategoryOptions{SavePath: "/s"}))
	require.Equal(t, 2, posted)
}

func TestCategoryWithOptionsVersionGateAcceptsMinimum(t *testing.T) {
	// WebAPI 2.8.4 (qBittorrent 4.4.0) is the first to read the download path
	// form fields, so it must pass the gate.
	posted := 0
	client := newCategoryTestClientVersion(t, "2.8.4", func(w http.ResponseWriter, _ *http.Request) {
		posted++
		w.WriteHeader(http.StatusOK)
	})
	ctx := context.Background()
	set := CategoryOptions{SavePath: "/s", DownloadPath: &CategoryDownloadPath{Enabled: true, Path: "/dl"}}

	require.NoError(t, client.CreateCategoryWithOptionsCtx(ctx, "c", set))
	require.NoError(t, client.EditCategoryWithOptionsCtx(ctx, "c", set))
	require.Equal(t, 2, posted)
}

func TestMainDataFullUpdateClearsCategoryDownloadPath(t *testing.T) {
	// Before WebAPI 2.11.10 a partial update cannot report a path going back
	// to inherit, so the full update is what clears it.
	responses := []string{
		`{"rid":1,"full_update":true,"categories":{"movies":{"name":"movies","savePath":"/m","download_path":"/dl"}}}`,
		`{"rid":2,"full_update":true,"categories":{"movies":{"name":"movies","savePath":"/m"}}}`,
	}
	step := 0
	client := newCategoryTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[step]))
		step++
	})

	ctx := context.Background()
	data := &MainData{}
	require.NoError(t, data.Update(ctx, client))
	require.Equal(t, &CategoryDownloadPath{Enabled: true, Path: "/dl"}, data.Categories["movies"].DownloadPath)

	require.NoError(t, data.Update(ctx, client))
	require.Nil(t, data.Categories["movies"].DownloadPath)
}

func TestSyncSnapshotsCopyCategoryDownloadPath(t *testing.T) {
	sm := &SyncManager{data: &MainData{Categories: map[string]Category{
		"movies": {Name: "movies", DownloadPath: &CategoryDownloadPath{Enabled: true, Path: "/dl/movies"}},
		"tv":     {Name: "tv"},
	}}}

	categories := sm.GetCategoriesUnchecked()
	categories["movies"].DownloadPath.Path = "/changed"
	require.Equal(t, "/dl/movies", sm.data.Categories["movies"].DownloadPath.Path)
	require.Nil(t, categories["tv"].DownloadPath)

	data := sm.copyMainData(sm.data)
	data.Categories["movies"].DownloadPath.Path = "/changed"
	require.Equal(t, "/dl/movies", sm.data.Categories["movies"].DownloadPath.Path)
}
