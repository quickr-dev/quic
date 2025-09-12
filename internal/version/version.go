package version

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Version is set at build time via ldflags
var Version = "dev"

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// GetLatestVersion gets the latest available version from VERSION file in public repo
func GetLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Get VERSION file directly
	req, err := http.NewRequest("GET", "https://raw.githubusercontent.com/quickr-dev/quic-cli/main/VERSION", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to check version: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	version := strings.TrimSpace(string(body))
	if version == "" {
		return "", fmt.Errorf("VERSION file is empty")
	}

	// Remove 'v' prefix if present
	return strings.TrimPrefix(version, "v"), nil
}

// compareVersions compares two version strings (without 'v' prefix)
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	// Pad arrays to same length
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for len(aParts) < maxLen {
		aParts = append(aParts, "0")
	}
	for len(bParts) < maxLen {
		bParts = append(bParts, "0")
	}

	for i := 0; i < maxLen; i++ {
		if aParts[i] < bParts[i] {
			return -1
		} else if aParts[i] > bParts[i] {
			return 1
		}
	}

	return 0
}

// IsNewerVersion compares two version strings
func IsNewerVersion(current, latest string) bool {
	if current == "dev" {
		return false // Don't update dev builds
	}

	// Simple version comparison - assumes semantic versioning
	currentParts := strings.Split(strings.TrimPrefix(current, "v"), ".")
	latestParts := strings.Split(strings.TrimPrefix(latest, "v"), ".")

	// Pad arrays to same length
	maxLen := len(currentParts)
	if len(latestParts) > maxLen {
		maxLen = len(latestParts)
	}

	for len(currentParts) < maxLen {
		currentParts = append(currentParts, "0")
	}
	for len(latestParts) < maxLen {
		latestParts = append(latestParts, "0")
	}

	for i := 0; i < maxLen; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		} else if latestParts[i] < currentParts[i] {
			return false
		}
	}

	return false
}

// SelfUpdate downloads and replaces the current binary
func SelfUpdate() error {
	latest, err := GetLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check latest version: %v", err)
	}

	if !IsNewerVersion(Version, latest) {
		return fmt.Errorf("already on latest version %s", Version)
	}

	// Determine binary name for current platform
	var binaryName string
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		binaryName = "quic-darwin-amd64"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		binaryName = "quic-darwin-arm64"
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		binaryName = "quic-linux-amd64"
	default:
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Download from public quic-cli repo bin/ directory (no auth needed)
	downloadURL := fmt.Sprintf("https://github.com/quickr-dev/quic-cli/raw/main/bin/%s", binaryName)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download update: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download update: HTTP %d", resp.StatusCode)
	}

	// Get current executable path
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	// Create temporary file
	tmpFile := executable + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %v", err)
	}

	// Copy downloaded content
	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to write update: %v", err)
	}

	// Replace current executable
	if err := os.Rename(tmpFile, executable); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to replace executable: %v", err)
	}

	fmt.Println("Done")

	return nil
}

// RunCurlInstall runs the curl install script to update quic
func RunCurlInstall() error {
	cmd := exec.Command("bash", "-c", "curl -sf https://raw.githubusercontent.com/quickr-dev/quic-cli/main/install.sh | bash")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CheckForUpdates checks if an update is available and runs curl install script
func CheckForUpdates() {
	latest, err := GetLatestVersion()
	if err != nil {
		// Silently fail - don't interrupt user workflow
		return
	}

	if IsNewerVersion(Version, latest) {
		fmt.Printf("Updating quic %s -> %s...\n", Version, latest)
		if err := RunCurlInstall(); err != nil {
			fmt.Printf("Update failed: %v\n", err)
		}
	}
}
