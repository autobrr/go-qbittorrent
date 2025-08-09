package qbittorrent

import (
	"context"
	"testing"
	"time"
)

func TestSyncManager_BasicSync(t *testing.T) {
	client := &Client{}
	syncManager := NewSyncManager(client)

	if syncManager == nil {
		t.Fatal("NewSyncManager returned nil")
	}

	if syncManager.client != client {
		t.Fatal("SyncManager client not set correctly")
	}

	// Test default options
	if syncManager.options.SyncInterval != 2*time.Second {
		t.Errorf("Expected default sync interval of 2s, got %v", syncManager.options.SyncInterval)
	}
}

func TestSyncManager_WithOptions(t *testing.T) {
	client := &Client{}
	options := SyncOptions{
		AutoSync:     true,
		SyncInterval: 5 * time.Second,
	}

	syncManager := NewSyncManager(client, options)

	if !syncManager.options.AutoSync {
		t.Error("AutoSync option not set correctly")
	}

	if syncManager.options.SyncInterval != 5*time.Second {
		t.Errorf("Expected sync interval of 5s, got %v", syncManager.options.SyncInterval)
	}
}

func TestSyncManager_GetDataWhenEmpty(t *testing.T) {
	client := &Client{}
	syncManager := NewSyncManager(client)

	data := syncManager.GetData()
	if data != nil {
		t.Error("Expected nil data when not initialized")
	}

	torrents := syncManager.GetTorrents()
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
	client := &Client{}
	syncManager := NewSyncManager(client)

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
		Tags:     []string{"tag1", "tag2"},
		Trackers: map[string][]string{
			"abc123": {"http://tracker1.com", "http://tracker2.com"},
		},
		ServerState: ServerState{
			ConnectionStatus: "connected",
			DlInfoSpeed:     100000,
			UpInfoSpeed:     50000,
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
	torrents := syncManager.GetTorrents()
	if torrents == nil {
		t.Fatal("Expected torrents map, got nil")
	}
	if len(torrents) != 1 {
		t.Errorf("Expected 1 torrent, got %d", len(torrents))
	}
	if torrent, exists := torrents["abc123"]; !exists {
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
	torrentsMap := map[string]interface{}{
		"abc123": map[string]interface{}{
			"progress": 0.75,
			"dlspeed":  float64(1500),
			"state":    "downloading",
			// Note: upspeed, category, etc. are NOT present in this update
		},
	}

	sm.mergeTorrents(torrentsMap)

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

	// Add a new torrent through mergeTorrents
	torrentsMap := map[string]interface{}{
		"def456": map[string]interface{}{
			"hash":     "def456",
			"name":     "New Torrent",
			"progress": 0.25,
			"dlspeed":  float64(2000),
			"state":    "downloading",
			"category": "movies",
		},
	}

	sm.mergeTorrents(torrentsMap)

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
	client := &Client{}
	
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
		Tags:     []string{"tag1", "tag2"},
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
	client := &Client{}
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
	client := &Client{}
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
	client := &Client{}
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
	client := &Client{}
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

