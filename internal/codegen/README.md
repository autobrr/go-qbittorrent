# Code Generation Tools

This directory contains code generation tools for the go-qbittorrent project.

## Tools

### generate_maindata_updaters.go

Generates type-safe field update methods for MainData partial updates.

**Purpose**: Creates methods that only update fields present in JSON response data, avoiding overwriting fields with zero values when they're not included in the response.

**Input**: Parses `domain.go` to extract struct definitions for:
- `Torrent`
- `ServerState` 
- `Category`
- `TorrentTracker`

**Output**: Generates `maindata_updaters_generated.go` in the project root with methods like:
- `updateTorrentFields()`
- `updateServerStateFields()`
- `updateCategoryFields()`
- `updateTorrentTrackerFields()`

**Usage**: Run from project root with `go generate`

### generate_torrent_filter.go

Generates torrent sorting functions.

**Purpose**: Creates efficient sorting logic for torrent lists based on various fields, supporting both ascending and descending order.

**Input**: Parses `domain.go` to extract the `Torrent` struct fields and their JSON tags.

**Output**: Generates `filter_generated.go` in the project root with:
- `applyTorrentSorting()` function that handles sorting by any torrent field
- Support for boolean fields, comparable types (int, string, etc.), and custom types like `TorrentState`

**Usage**: Run from project root with `go generate`

## How It Works

1. The generator uses Go's AST parsing to analyze struct definitions
2. Extracts field names, types, and JSON tags
3. Generates type-safe update methods that:
   - Check if a field exists in the update map
   - Perform type assertions
   - Only update fields that are present in the JSON response
4. Handles complex types including slices and custom enums

This ensures that partial updates from qBittorrent's sync API don't overwrite existing data with empty/zero values.

1. The generator uses Go's AST parsing to analyze struct definitions
2. Extracts field names, types, and JSON tags
3. Generates type-safe update methods that:
   - Check if a field exists in the update map
   - Perform type assertions
   - Only update fields that are present in the JSON response
4. Handles complex types including slices and custom enums

This ensures that partial updates from qBittorrent's sync API don't overwrite existing data with empty/zero values.
