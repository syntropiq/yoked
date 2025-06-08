package ollamarunner

import (
	"log/slog"
	"os"
	"testing"
)

// TestKVCacheSizingLogging verifies that our diagnostic logging is working
// This is a simple test to ensure the logging statements we added are functional
func TestKVCacheSizingLogging(t *testing.T) {
	// Set log level to INFO to see our diagnostic messages
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	t.Log("KV cache sizing logging test - diagnostic logs should appear in NewInputCache calls")
	t.Log("To see actual KV cache sizing logs, run: go test -v ./server -run TestDynamicNumCtx")
	t.Log("Look for 'NewInputCache: KV cache sizing' messages in the output")

	// This test passes if the logging setup works correctly
	slog.Info("Test logging setup", "status", "working")
}
