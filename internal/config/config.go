package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	SelectedServer  string                   `json:"selected_server"`
	SelectedRestore string                   `json:"selected_restore,omitempty"`
	AuthToken       string                   `json:"auth_token"`
	LastServerCheck time.Time                `json:"last_server_check"`
	Servers         map[string]ServerMetrics `json:"servers"`
}

type ServerMetrics struct {
	LastLatencyMS int       `json:"last_latency_ms"`
	LastSuccess   time.Time `json:"last_success"`
}

const (
	ConfigDirName  = "quic"
	ConfigFileName = "config.json"
)

var AvailableServers = []string{
	"lhr.quickr.dev",
	// "gru.quickr.dev",
}

func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Use XDG config directory if available
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, ConfigDirName), nil
	}

	return filepath.Join(homeDir, ".config", ConfigDirName), nil
}

func getConfigPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, ConfigFileName), nil
}

func Load() (*Config, error) {
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

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) Save() error {
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

func createDefaultConfig() (*Config, error) {
	// Test both servers and pick the fastest
	bestServer := selectBestServer(AvailableServers)
	if bestServer == "" {
		return nil, fmt.Errorf("no servers are reachable")
	}

	config := &Config{
		SelectedServer:  bestServer,
		LastServerCheck: time.Now(),
		Servers:         make(map[string]ServerMetrics),
	}

	// Test all servers and save metrics
	for _, server := range AvailableServers {
		latency, err := testServerLatency(server)
		if err == nil {
			config.Servers[server] = ServerMetrics{
				LastLatencyMS: int(latency.Milliseconds()),
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

func selectBestServer(servers []string) string {
	type result struct {
		server   string
		duration time.Duration
		err      error
	}

	results := make(chan result, len(servers))

	for _, server := range servers {
		go func(s string) {
			duration, err := testServerLatency(s)
			results <- result{s, duration, err}
		}(server)
	}

	// Return fastest successful connection
	var fastest result
	for i := 0; i < len(servers); i++ {
		r := <-results
		if r.err == nil && (fastest.server == "" || r.duration < fastest.duration) {
			fastest = r
		}
	}
	return fastest.server
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
