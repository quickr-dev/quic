package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type UserConfig struct {
	AuthToken       string                 `json:"authToken"`
	SelectedHost    string                 `json:"selectedHost"`
	DefaultTemplate string                 `json:"defaultTemplate,omitempty"`
	LastServerCheck time.Time              `json:"lastServerCheck"`
	HostMetrics     map[string]hostMetrics `json:"hostsMetrics"`
}

type hostMetrics struct {
	LastLatencyMS int       `json:"last_latency_ms"`
	LastSuccess   time.Time `json:"last_success"`
}

const (
	configDirName  = "quic"
	configFileName = "config.json"
)

func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Use XDG config directory if available
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, configDirName), nil
	}

	return filepath.Join(homeDir, ".config", configDirName), nil
}

func getConfigPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, configFileName), nil
}

func LoadUserConfig() (*UserConfig, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	// If config doesn't exist, create default
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return createDefaultConfig()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config UserConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *UserConfig) Save() error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	// Ensure config directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func createDefaultConfig() (*UserConfig, error) {
	// Load project config to get available hosts
	projectConfig, err := LoadProjectConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load project config: %w", err)
	}

	if len(projectConfig.Hosts) == 0 {
		return nil, fmt.Errorf("no hosts configured in project config")
	}

	// Extract host IPs from project config
	hostIPs := make([]string, len(projectConfig.Hosts))
	for i, host := range projectConfig.Hosts {
		hostIPs[i] = host.IP
	}

	host, hostResults := getLowestLatencyHost(hostIPs)
	if host == "" {
		return nil, fmt.Errorf("no servers are reachable")
	}

	config := &UserConfig{
		SelectedHost:    host,
		LastServerCheck: time.Now(),
		HostMetrics:     make(map[string]hostMetrics),
	}

	for server, result := range hostResults {
		if result.err == nil {
			config.HostMetrics[server] = hostMetrics{
				LastLatencyMS: int(result.duration.Milliseconds()),
				LastSuccess:   time.Now(),
			}
		}
	}

	// Save the config
	if err := config.Save(); err != nil {
		return nil, err
	}

	return config, nil
}

type result struct {
	server   string
	duration time.Duration
	err      error
}

func getLowestLatencyHost(ips []string) (string, map[string]result) {
	results := make(chan result, len(ips))
	for _, server := range ips {
		go func(s string) {
			duration, err := testServerLatency(s)
			results <- result{s, duration, err}
		}(server)
	}

	serverResults := make(map[string]result)
	for range ips {
		r := <-results
		serverResults[r.server] = r
	}

	var fastestServer string
	var fastestDuration time.Duration
	for server, r := range serverResults {
		if r.err == nil && (fastestServer == "" || r.duration < fastestDuration) {
			fastestServer = server
			fastestDuration = r.duration
		}
	}
	return fastestServer, serverResults
}

func testServerLatency(server string) (time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", server+":8443", 5*time.Second)
	if err != nil {
		return 0, err
	}
	conn.Close()
	return time.Since(start), nil
}

