package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// Version is set at build time via ldflags
var Version = "dev"

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func GetLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("GET", "https://api.github.com/repos/quickr-dev/quic/releases/latest", nil)
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

	// Parse JSON response to get tag_name
	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("failed to parse release info: %v", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no release tag found")
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

func IsNewerVersion(current, latest string) bool {
	if current == "dev" {
		return false
	}

	currentParts := strings.Split(strings.TrimPrefix(current, "v"), ".")
	latestParts := strings.Split(strings.TrimPrefix(latest, "v"), ".")

	maxLen := max(len(latestParts), len(currentParts))

	for len(currentParts) < maxLen {
		currentParts = append(currentParts, "0")
	}
	for len(latestParts) < maxLen {
		latestParts = append(latestParts, "0")
	}

	for i := range maxLen {
		if latestParts[i] > currentParts[i] {
			return true
		} else if latestParts[i] < currentParts[i] {
			return false
		}
	}

	return false
}

func SelfUpdate() error {
	latest, err := GetLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check latest version: %v", err)
	}

	if !IsNewerVersion(Version, latest) {
		return fmt.Errorf("already on latest version %s", Version)
	}

	var binaryName string
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		binaryName = "quic-darwin-amd64"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		binaryName = "quic-darwin-arm64"
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		binaryName = "quic-linux-amd64"
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		binaryName = "quic-linux-arm64"
	default:
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	downloadURL := fmt.Sprintf("https://github.com/quickr-dev/quic/releases/latest/download/%s", binaryName)

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

func CheckForUpdateNotification() {
	latest, err := GetLatestVersion()
	if err != nil {
		return
	}

	if IsNewerVersion(Version, latest) {
		fmt.Printf("> A newer version is available: v%s\n", latest)
		fmt.Println("> Run 'quic update' to update")
	}
}
