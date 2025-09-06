package e2e_cli

import (
	"os"
	"testing"
)

func cleanupQuicConfig(t *testing.T) {
	os.Remove("quic.json")
}

func requireFile(t *testing.T, path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Expected file %s to exist", path)
	}
}