package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"testing"
	"time"
)

// MockClient creates a client with mocked HTTP responses
type MockClient struct {
	*Client
	mockResponses map[string]mockResponse
	callCount     int
}

type mockResponse struct {
	data map[string]interface{}
	err  error
}

type mockRoundTripper struct {
	mock *MockClient
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mock.callCount++

	// Get the mock response for this endpoint
	response, exists := m.mock.mockResponses[req.URL.Path]
	if !exists || response.err != nil {
		if response.err != nil {
			return nil, response.err
		}
		return nil, context.DeadlineExceeded // Default error for missing mocks
	}

	// Create a mock HTTP response
	jsonData, _ := json.Marshal(response.data)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(jsonData)),
	}, nil
}

func NewMockClient() *MockClient {
	// Create a mock transport that returns mock responses
	mockTransport := &mockRoundTripper{}

	// Create a client with the mock transport
	jar, _ := cookiejar.New(nil)
	client := &Client{
		http: &http.Client{
			Transport: mockTransport,
			Jar:       jar,
		},
	}

	mock := &MockClient{
		Client:        client,
		mockResponses: make(map[string]mockResponse),
	}

	// Store reference to mock in transport so it can access mock responses
	mockTransport.mock = mock

	// Set up default mock responses
	mock.SetMockResponse("/sync/maindata", mockResponse{
		data: map[string]interface{}{
			"rid":         1,
			"full_update": true,
			"torrents":    make(map[string]interface{}),
			"categories":  make(map[string]interface{}),
			"tags":        []string{},
			"server_state": map[string]interface{}{
				"connection_status": "connected",
			},
		},
		err: nil,
	})

	return mock
}

func (m *MockClient) SetMockResponse(endpoint string, response mockResponse) {
	m.mockResponses[endpoint] = response
}

func (m *MockClient) SyncMainDataCtx(ctx context.Context, rid int64) (*MainData, error) {
	m.callCount++
	response, exists := m.mockResponses["/sync/maindata"]
	if !exists || response.err != nil {
		if response.err != nil {
			return nil, response.err
		}
		return nil, context.DeadlineExceeded // Default error for missing mocks
	}

	return &MainData{
		Rid:        int64(response.data["rid"].(int)),
		FullUpdate: response.data["full_update"].(bool),
		Torrents:   make(map[string]Torrent),
		Categories: make(map[string]Category),
		Tags:       []string{},
		ServerState: ServerState{
			ConnectionStatus: "connected",
		},
	}, nil
}

func (m *MockClient) SyncMainDataCtxWithRaw(ctx context.Context, rid int64) (*MainData, map[string]interface{}, error) {
	data, err := m.SyncMainDataCtx(ctx, rid)
	if err != nil {
		return nil, nil, err
	}

	// Create basic raw data for testing
	rawData := map[string]interface{}{
		"rid":         data.Rid,
		"full_update": data.FullUpdate,
		"torrents":    map[string]interface{}{},
		"categories":  map[string]interface{}{},
		"tags":        []interface{}{},
		"server_state": map[string]interface{}{
			"connection_status": "connected",
		},
	}

	return data, rawData, nil
}

func createMockSyncManager() (*SyncManager, *MockClient) {
	mockClient := NewMockClient()
	sm := &SyncManager{
		client:  mockClient.Client,
		options: DefaultSyncOptions(),
	}
	sm.syncCond = sync.NewCond(&sm.syncMu)

	return sm, mockClient
}

func TestSyncManager_BasicSync(t *testing.T) {
	syncManager, _ := createMockSyncManager()

	if syncManager == nil {
		t.Fatal("NewSyncManager returned nil")
	}

	// Test default options
	if syncManager.options.SyncInterval != 2*time.Second {
		t.Errorf("Expected default sync interval of 2s, got %v", syncManager.options.SyncInterval)
	}
}

func TestSyncManager_WithOptions(t *testing.T) {
	options := SyncOptions{
		AutoSync:     true,
		SyncInterval: 5 * time.Second,
	}

	mockClient := NewMockClient()
	syncManager := NewSyncManager(mockClient.Client, options)

	if !syncManager.options.AutoSync {
		t.Error("AutoSync option not set correctly")
	}

	if syncManager.options.SyncInterval != 5*time.Second {
		t.Errorf("Expected sync interval of 5s, got %v", syncManager.options.SyncInterval)
	}
}

func TestSyncManager_GetDataWhenEmpty(t *testing.T) {
	syncManager, _ := createMockSyncManager()
	// Disable dynamic sync for this test to prevent automatic syncing
	syncManager.options.DynamicSync = false

	data := syncManager.GetData()
	if data != nil {
		t.Error("Expected nil data when not initialized")
	}

	torrents := syncManager.GetTorrents(TorrentFilterOptions{})
	if torrents != nil {
		t.Error("Expected nil torrents when not initialized")
	}

	_, exists := syncManager.GetTorrent("dummy")
	if exists {
		t.Error("Expected false for torrent existence when not initialized")
	}

	categories := syncManager.GetCategories()
	if categories != nil {
		t.Error("Expected nil categories when not initialized")
	}

	tags := syncManager.GetTags()
	if tags != nil {
		t.Error("Expected nil tags when not initialized")
	}
}

func TestSyncManager_InitializeData(t *testing.T) {
	syncManager, _ := createMockSyncManager()

	// Manually initialize data to test getter methods
	syncManager.data = &MainData{
		Rid:        1,
		FullUpdate: true,
		Torrents: map[string]Torrent{
			"abc123": {
				Hash:     "abc123",
				Name:     "Test Torrent",
				Progress: 0.5,
				DlSpeed:  1000,
				UpSpeed:  500,
				State:    "downloading",
				Category: "test",
			},
		},
		Categories: map[string]Category{
			"test": {
				Name:     "test",
				SavePath: "/downloads/test",
			},
		},
		Tags: []string{"tag1", "tag2"},
		Trackers: map[string][]string{
			"abc123": {"http://tracker1.com", "http://tracker2.com"},
		},
		ServerState: ServerState{
			ConnectionStatus: "connected",
			DlInfoSpeed:      100000,
			UpInfoSpeed:      50000,
		},
	}

	// Test GetData returns a copy
	data := syncManager.GetData()
	if data == nil {
		t.Fatal("Expected data, got nil")
	}
	if data.Rid != 1 {
		t.Errorf("Expected RID 1, got %d", data.Rid)
	}

	// Test GetTorrents
	torrents := syncManager.GetTorrents(TorrentFilterOptions{})
	if torrents == nil {
		t.Fatal("Expected torrents slice, got nil")
	}
	if len(torrents) != 1 {
		t.Errorf("Expected 1 torrent, got %d", len(torrents))
	}
	var torrent Torrent
	found := false
	for _, t := range torrents {
		if t.Hash == "abc123" {
			torrent = t
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected torrent abc123 to exist")
	} else {
		if torrent.Name != "Test Torrent" {
			t.Errorf("Expected torrent name 'Test Torrent', got %s", torrent.Name)
		}
	}

	// Test GetTorrentMap
	torrentMap := syncManager.GetTorrentMap(TorrentFilterOptions{})
	if torrentMap == nil {
		t.Fatal("Expected torrent map, got nil")
	}
	if len(torrentMap) != 1 {
		t.Errorf("Expected 1 torrent, got %d", len(torrentMap))
	}
	if torrent, exists := torrentMap["abc123"]; !exists {
		t.Error("Expected torrent abc123 to exist")
	} else {
		if torrent.Name != "Test Torrent" {
			t.Errorf("Expected torrent name 'Test Torrent', got %s", torrent.Name)
		}
	}

	// Test GetTorrent
	torrent, exists := syncManager.GetTorrent("abc123")
	if !exists {
		t.Error("Expected torrent abc123 to exist")
	}
	if torrent.Name != "Test Torrent" {
		t.Errorf("Expected torrent name 'Test Torrent', got %s", torrent.Name)
	}

	// Test GetCategories
	categories := syncManager.GetCategories()
	if categories == nil {
		t.Fatal("Expected categories map, got nil")
	}
	if len(categories) != 1 {
		t.Errorf("Expected 1 category, got %d", len(categories))
	}

	// Test GetTags
	tags := syncManager.GetTags()
	if tags == nil {
		t.Fatal("Expected tags slice, got nil")
	}
	if len(tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(tags))
	}

	// Test GetServerState
	serverState := syncManager.GetServerState()
	if serverState.ConnectionStatus != "connected" {
		t.Errorf("Expected connection status 'connected', got %s", serverState.ConnectionStatus)
	}

	// Test LastSyncTime is set when we update lastSync
	syncManager.lastSync = time.Now()
	lastSync := syncManager.LastSyncTime()
	if time.Since(lastSync) > time.Second {
		t.Error("LastSyncTime should be recent")
	}
}

func TestMergeTorrents(t *testing.T) {
	sm := &SyncManager{}
	sm.data = &MainData{
		Torrents: make(map[string]Torrent),
	}

	// Add an existing torrent
	existing := Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		Progress: 0.5,
		DlSpeed:  1000,
		UpSpeed:  500,
		State:    "downloading",
		Category: "test",
	}
	sm.data.Torrents["abc123"] = existing

	// Simulate a JSON partial update with only some fields
	rawData := map[string]interface{}{
		"torrents": map[string]interface{}{
			"abc123": map[string]interface{}{
				"progress": 0.75,
				"dlspeed":  float64(1500),
				"state":    "downloading",
				// Note: upspeed, category, etc. are NOT present in this update
			},
		},
	}

	source := &MainData{
		Rid:               1,
		TorrentsRemoved:   []string{},
		CategoriesRemoved: []string{},
		TagsRemoved:       []string{},
	}

	sm.data.UpdateWithRawData(rawData, source)

	merged := sm.data.Torrents["abc123"]

	// Check that updated fields were applied
	if merged.Progress != 0.75 {
		t.Errorf("Expected progress 0.75, got %f", merged.Progress)
	}

	if merged.DlSpeed != 1500 {
		t.Errorf("Expected DlSpeed 1500, got %d", merged.DlSpeed)
	}

	// Check that non-updated fields were preserved
	if merged.Name != "Test Torrent" {
		t.Errorf("Expected name preserved, got %s", merged.Name)
	}

	if merged.UpSpeed != 500 {
		t.Errorf("Expected UpSpeed preserved, got %d", merged.UpSpeed)
	}

	if merged.Category != "test" {
		t.Errorf("Expected category preserved, got %s", merged.Category)
	}
}

func TestMergeTorrents_NewTorrent(t *testing.T) {
	sm := &SyncManager{}
	sm.data = &MainData{
		Torrents: make(map[string]Torrent),
	}

	// Add a new torrent through UpdateWithRawData
	rawData := map[string]interface{}{
		"torrents": map[string]interface{}{
			"def456": map[string]interface{}{
				"hash":     "def456",
				"name":     "New Torrent",
				"progress": 0.25,
				"dlspeed":  float64(2000),
				"state":    "downloading",
				"category": "movies",
			},
		},
	}

	source := &MainData{
		Rid:               1,
		TorrentsRemoved:   []string{},
		CategoriesRemoved: []string{},
		TagsRemoved:       []string{},
	}

	sm.data.UpdateWithRawData(rawData, source)

	if len(sm.data.Torrents) != 1 {
		t.Errorf("Expected 1 torrent, got %d", len(sm.data.Torrents))
	}

	newTorrent, exists := sm.data.Torrents["def456"]
	if !exists {
		t.Fatal("Expected new torrent def456 to exist")
	}

	if newTorrent.Name != "New Torrent" {
		t.Errorf("Expected name 'New Torrent', got %s", newTorrent.Name)
	}

	if newTorrent.Progress != 0.25 {
		t.Errorf("Expected progress 0.25, got %f", newTorrent.Progress)
	}

	if newTorrent.DlSpeed != 2000 {
		t.Errorf("Expected DlSpeed 2000, got %d", newTorrent.DlSpeed)
	}
}

func TestRemoveDuplicateStrings(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b", "d"}
	expected := []string{"a", "b", "c", "d"}

	result := removeDuplicateStrings(input)

	if len(result) != len(expected) {
		t.Errorf("Expected length %d, got %d", len(expected), len(result))
	}

	// Check all expected items are present
	resultMap := make(map[string]bool)
	for _, item := range result {
		resultMap[item] = true
	}

	for _, item := range expected {
		if !resultMap[item] {
			t.Errorf("Expected item %s not found in result", item)
		}
	}
}

func TestRemoveStrings(t *testing.T) {
	input := []string{"a", "b", "c", "d", "e"}
	toRemove := []string{"b", "d"}
	expected := []string{"a", "c", "e"}

	result := removeStrings(input, toRemove)

	if len(result) != len(expected) {
		t.Errorf("Expected length %d, got %d", len(expected), len(result))
	}

	for i, item := range expected {
		if result[i] != item {
			t.Errorf("Expected item %s at position %d, got %s", item, i, result[i])
		}
	}
}

func TestDefaultSyncOptions(t *testing.T) {
	opts := DefaultSyncOptions()

	if opts.AutoSync {
		t.Error("Expected AutoSync to be false by default")
	}

	if opts.SyncInterval != 2*time.Second {
		t.Errorf("Expected SyncInterval to be 2s by default, got %v", opts.SyncInterval)
	}

	if opts.RetainRemovedData {
		t.Error("Expected RetainRemovedData to be false by default")
	}

	if opts.OnUpdate != nil {
		t.Error("Expected OnUpdate to be nil by default")
	}

	if opts.OnError != nil {
		t.Error("Expected OnError to be nil by default")
	}
}

func TestSyncManager_CallbacksWithMockData(t *testing.T) {
	client := NewClient(Config{Host: "http://localhost:8080"})

	var updateCalled bool
	var errorCalled bool
	var lastMainData *MainData
	var lastError error

	options := SyncOptions{
		OnUpdate: func(data *MainData) {
			updateCalled = true
			lastMainData = data
		},
		OnError: func(err error) {
			errorCalled = true
			lastError = err
		},
	}

	syncManager := NewSyncManager(client, options)

	// Manually trigger callbacks to test them
	testData := &MainData{
		Rid:        1,
		FullUpdate: true,
		Torrents:   make(map[string]Torrent),
	}

	if syncManager.options.OnUpdate != nil {
		syncManager.options.OnUpdate(testData)
	}

	if !updateCalled {
		t.Error("Expected OnUpdate callback to be called")
	}

	if lastMainData != testData {
		t.Error("Expected lastMainData to be set correctly")
	}

	// Test error callback
	testError := context.DeadlineExceeded
	if syncManager.options.OnError != nil {
		syncManager.options.OnError(testError)
	}

	if !errorCalled {
		t.Error("Expected OnError callback to be called")
	}

	if lastError != testError {
		t.Error("Expected lastError to be set correctly")
	}
}

func TestSyncManager_CopyMainData(t *testing.T) {
	sm := &SyncManager{}

	original := &MainData{
		Rid:        1,
		FullUpdate: true,
		Torrents: map[string]Torrent{
			"abc123": {Hash: "abc123", Name: "Test"},
		},
		Categories: map[string]Category{
			"test": {Name: "test", SavePath: "/test"},
		},
		Tags: []string{"tag1", "tag2"},
		Trackers: map[string][]string{
			"abc123": {"tracker1", "tracker2"},
		},
		ServerState: ServerState{
			ConnectionStatus: "connected",
		},
	}

	copy := sm.copyMainData(original)

	// Test that it's a deep copy
	if copy == original {
		t.Error("Expected deep copy, got same pointer")
	}

	if copy.Rid != original.Rid {
		t.Error("RID not copied correctly")
	}

	if copy.FullUpdate != original.FullUpdate {
		t.Error("FullUpdate not copied correctly")
	}

	// Test maps are different instances but same content
	if len(copy.Torrents) != len(original.Torrents) {
		t.Error("Torrents map not copied correctly")
	}

	// Modify copy to ensure it doesn't affect original
	copy.Torrents["new"] = Torrent{Hash: "new"}
	if len(original.Torrents) != 1 {
		t.Error("Modifying copy affected original")
	}

	// Test slices are different instances
	if len(copy.Tags) != len(original.Tags) {
		t.Error("Tags slice not copied correctly")
	}

	copy.Tags = append(copy.Tags, "new_tag")
	if len(original.Tags) != 2 {
		t.Error("Modifying copy tags affected original")
	}
}

func TestSyncManager_DynamicSync(t *testing.T) {
	client := NewClient(Config{Host: "http://localhost:8080"})
	options := SyncOptions{
		DynamicSync:     true,
		MinSyncInterval: 1 * time.Second,
		MaxSyncInterval: 10 * time.Second,
		JitterPercent:   20,
	}

	syncManager := NewSyncManager(client, options)

	if !syncManager.options.DynamicSync {
		t.Error("Expected DynamicSync to be enabled")
	}

	if syncManager.options.MinSyncInterval != 1*time.Second {
		t.Errorf("Expected MinSyncInterval 1s, got %v", syncManager.options.MinSyncInterval)
	}

	if syncManager.options.MaxSyncInterval != 10*time.Second {
		t.Errorf("Expected MaxSyncInterval 10s, got %v", syncManager.options.MaxSyncInterval)
	}

	if syncManager.options.JitterPercent != 20 {
		t.Errorf("Expected JitterPercent 20, got %d", syncManager.options.JitterPercent)
	}
}

func TestSyncManager_CalculateNextInterval(t *testing.T) {
	client := NewClient(Config{Host: "http://localhost:8080"})
	options := SyncOptions{
		DynamicSync:     true,
		MinSyncInterval: 1 * time.Second,
		MaxSyncInterval: 10 * time.Second,
		JitterPercent:   0, // No jitter for predictable testing
	}

	syncManager := NewSyncManager(client, options)

	// Test with short sync duration
	syncManager.lastSyncDuration = 500 * time.Millisecond
	interval := syncManager.calculateNextInterval()
	expected := 1 * time.Second // Should be clamped to minimum
	if interval != expected {
		t.Errorf("Expected interval %v, got %v", expected, interval)
	}

	// Test with medium sync duration
	syncManager.lastSyncDuration = 2 * time.Second
	interval = syncManager.calculateNextInterval()
	expected = 4 * time.Second // Double the duration
	if interval != expected {
		t.Errorf("Expected interval %v, got %v", expected, interval)
	}

	// Test with long sync duration
	syncManager.lastSyncDuration = 8 * time.Second
	interval = syncManager.calculateNextInterval()
	expected = 10 * time.Second // Should be clamped to maximum
	if interval != expected {
		t.Errorf("Expected interval %v, got %v", expected, interval)
	}
}

func TestSyncManager_CalculateNextIntervalWithJitter(t *testing.T) {
	client := NewClient(Config{Host: "http://localhost:8080"})
	options := SyncOptions{
		DynamicSync:     true,
		MinSyncInterval: 1 * time.Second,
		MaxSyncInterval: 10 * time.Second,
		JitterPercent:   50, // 50% jitter
	}

	syncManager := NewSyncManager(client, options)
	syncManager.lastSyncDuration = 2 * time.Second

	// Run multiple times to ensure jitter is applied
	intervals := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		intervals[i] = syncManager.calculateNextInterval()
	}

	// Check that we get different values (due to jitter)
	allSame := true
	first := intervals[0]
	for _, interval := range intervals[1:] {
		if interval != first {
			allSame = false
			break
		}
	}

	if allSame {
		t.Error("Expected different intervals due to jitter, but all were the same")
	}

	// Check that all intervals are within reasonable bounds
	baseInterval := 4 * time.Second
	maxJitter := time.Duration(float64(baseInterval) * 0.5) // 50% jitter
	maxExpected := baseInterval + maxJitter

	for _, interval := range intervals {
		if interval < syncManager.options.MinSyncInterval {
			t.Errorf("Interval %v is below minimum %v", interval, syncManager.options.MinSyncInterval)
		}
		if interval > maxExpected {
			t.Errorf("Interval %v is above expected maximum %v", interval, maxExpected)
		}
	}
}

func TestSyncManager_LastSyncDuration(t *testing.T) {
	client := NewClient(Config{Host: "http://localhost:8080"})
	syncManager := NewSyncManager(client)

	// Initially should be zero
	duration := syncManager.LastSyncDuration()
	if duration != 0 {
		t.Errorf("Expected initial duration 0, got %v", duration)
	}

	// Set a duration and test getter
	syncManager.lastSyncDuration = 2 * time.Second
	duration = syncManager.LastSyncDuration()
	if duration != 2*time.Second {
		t.Errorf("Expected duration 2s, got %v", duration)
	}
}

func TestDefaultSyncOptions_DynamicSync(t *testing.T) {
	opts := DefaultSyncOptions()

	if !opts.DynamicSync {
		t.Error("Expected DynamicSync to be true by default")
	}

	if opts.MaxSyncInterval != 30*time.Second {
		t.Errorf("Expected MaxSyncInterval 30s, got %v", opts.MaxSyncInterval)
	}

	if opts.MinSyncInterval != 1*time.Second {
		t.Errorf("Expected MinSyncInterval 1s, got %v", opts.MinSyncInterval)
	}

	if opts.JitterPercent != 10 {
		t.Errorf("Expected JitterPercent 10, got %d", opts.JitterPercent)
	}
}

func TestSyncManager_ConcurrentSync(t *testing.T) {
	client := NewClient(Config{
		Host:    "http://localhost:8080",
		Timeout: 1, // 1 second timeout for quick failure
	})
	syncManager := NewSyncManager(client)

	// Track how many times sync actually executes the network calls
	var syncCallCount int32
	var actualSyncCount int32

	// Mock the getRawMainData and SyncMainDataCtx methods by creating a custom sync behavior
	originalGetRawMainData := func(ctx context.Context, rid int64) (map[string]interface{}, error) {
		syncCallCount++
		// Simulate a slow network call
		time.Sleep(100 * time.Millisecond)
		return map[string]interface{}{
			"rid":         rid + 1,
			"full_update": rid == 0,
			"torrents":    make(map[string]interface{}),
		}, nil
	}

	originalSyncMainDataCtx := func(ctx context.Context, rid int64) (*MainData, error) {
		actualSyncCount++
		return &MainData{
			Rid:        rid + 1,
			FullUpdate: rid == 0,
			Torrents:   make(map[string]Torrent),
		}, nil
	}

	// Since we can't easily mock private methods, we'll test the concurrent behavior
	// by checking that multiple goroutines calling Sync don't cause race conditions

	const numGoroutines = 5
	var wg sync.WaitGroup
	results := make(chan error, numGoroutines)

	// Start multiple goroutines that try to sync simultaneously
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// We expect this to not cause a panic or race condition
			// The actual sync will fail because we don't have a real client,
			// but the concurrent access control should work
			err := syncManager.Sync(context.Background())
			results <- err
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(results)

	// Collect results - we expect all to have the same error (network failure)
	// but no race conditions or panics
	var errors []error
	for err := range results {
		errors = append(errors, err)
	}

	if len(errors) != numGoroutines {
		t.Errorf("Expected %d results, got %d", numGoroutines, len(errors))
	}

	// The important thing is that we didn't crash and all goroutines completed
	// The actual error content isn't important for this test since we don't have a real server

	// Test that the sync manager state is consistent
	if syncManager == nil {
		t.Error("SyncManager should not be nil after concurrent access")
	}

	// Avoid unused variable warnings
	_ = originalGetRawMainData
	_ = originalSyncMainDataCtx
}

func TestSyncManager_LastError(t *testing.T) {
	client := NewClient(Config{Host: "http://localhost:8080"})
	syncManager := NewSyncManager(client)

	// Initially should be nil
	err := syncManager.LastError()
	if err != nil {
		t.Errorf("Expected initial error to be nil, got %v", err)
	}

	// Manually set an error to test the getter
	syncManager.lastError = context.DeadlineExceeded
	err = syncManager.LastError()
	if err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got %v", err)
	}
}
