# go-qbittorrent

Go library for communicating with qBittorrent.

## Development

### Code Generation

This project uses Go's code generation to create type-safe field update methods for MainData partial updates. The generated code ensures that partial updates from qBittorrent's sync API don't overwrite existing data with zero values.

To regenerate code after modifying struct definitions in `domain.go`:

```bash
go generate
```

The code generator is located in `internal/codegen/` and outputs `maindata_updaters_generated.go`.
