package qbittorrent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterGeneratedIsUpToDate(t *testing.T) {
	assertGeneratedFileUpToDate(t, "internal/codegen/generate_torrent_filter.go", "filter_generated.go")
}

func TestMaindataUpdatersGeneratedIsUpToDate(t *testing.T) {
	assertGeneratedFileUpToDate(t, "internal/codegen/generate_maindata_updaters.go", "maindata_updaters_generated.go")
}

func TestAllGeneratedFilesAreUpToDate(t *testing.T) {
	t.Run("FilterGenerated", TestFilterGeneratedIsUpToDate)
	t.Run("MaindataUpdatersGenerated", TestMaindataUpdatersGeneratedIsUpToDate)
}

func assertGeneratedFileUpToDate(t *testing.T, generatorPath, generatedFile string) {
	t.Helper()

	existingContent, err := os.ReadFile(generatedFile)
	require.NoError(t, err, "Failed to read existing %s", generatedFile)

	cmd := exec.Command("go", "run", generatorPath)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	require.NoError(t, err, "Failed to run %s:\nstderr: %s", generatorPath, stderr.String())

	newContent, err := os.ReadFile(generatedFile)
	require.NoError(t, err, "Failed to read newly generated %s", generatedFile)

	if !bytes.Equal(existingContent, newContent) {
		require.NoError(t, os.WriteFile(generatedFile, existingContent, 0644), "Failed to restore original %s", generatedFile)
	}

	if !bytes.Equal(normalizeNewlines(existingContent), normalizeNewlines(newContent)) {
		require.FailNow(t, fmt.Sprintf("%s is not up to date. Please run: go generate ./...", generatedFile))
	}
}

func normalizeNewlines(content []byte) []byte {
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}
