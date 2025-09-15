package config

import (
	"encoding/json"
	"fmt"
	"maps"
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

type result struct {
	server   string
	duration time.Duration
	err      error
}

const (
	configDirName  = "quic"
	configFileName = "config.json"
)

func LoadUserConfig() (*UserConfig, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	var config *UserConfig

	// If config doesn't exist, create default structure
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config = createDefaultConfig()
	} else {
		// Load existing config
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}

		var loadedConfig UserConfig
		if err := json.Unmarshal(data, &loadedConfig); err != nil {
			return nil, err
		}
		config = &loadedConfig
	}

	// Validate and fix config issues
	configChanged, err := validateAndFixConfig(config)
	if err != nil {
		return nil, err
	}

	// Save config if any fixes were applied
	if configChanged {
		if err := config.Save(); err != nil {
			return nil, fmt.Errorf("failed to save fixed config: %w", err)
		}
	}

	return config, nil
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

func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

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

func selectBestHost() (string, map[string]hostMetrics, error) {
	// Load project config to get available hosts
	projectConfig, err := LoadProjectConfig()
	if err != nil {
		return "", nil, fmt.Errorf("failed to load project config: %w", err)
	}

	if len(projectConfig.Hosts) == 0 {
		return "", nil, fmt.Errorf("no hosts configured in project config")
	}

	// Extract host IPs from project config
	hostIPs := make([]string, len(projectConfig.Hosts))
	for i, host := range projectConfig.Hosts {
		hostIPs[i] = host.IP
	}

	host, hostResults := getLowestLatencyHost(hostIPs)
	if host == "" {
		return "", nil, fmt.Errorf("no servers are reachable")
	}

	// Build host metrics from results
	metrics := make(map[string]hostMetrics)
	for server, result := range hostResults {
		if result.err == nil {
			metrics[server] = hostMetrics{
				LastLatencyMS: int(result.duration.Milliseconds()),
				LastSuccess:   time.Now(),
			}
		}
	}

	return host, metrics, nil
}

func createDefaultConfig() *UserConfig {
	return &UserConfig{
		LastServerCheck: time.Now(),
		HostMetrics:     make(map[string]hostMetrics),
	}
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

func validateAndFixConfig(cfg *UserConfig) (bool, error) {
	// Auth token missing - can't fix this, return error
	if cfg.AuthToken == "" {
		return false, fmt.Errorf("no auth token configured. Please run 'quic login --token <token>'")
	}

	var configChanged bool

	// Check if we need to select/fix the host
	needsHostSelection := false

	if cfg.SelectedHost == "" {
		needsHostSelection = true
	} else {
		// Check if host exists in project config
		projectConfig, err := LoadProjectConfig()
		if err != nil {
			return false, fmt.Errorf("failed to load project config: %w", err)
		}

		hostConfig := projectConfig.GetHostByIP(cfg.SelectedHost)
		if hostConfig == nil {
			needsHostSelection = true
		}
	}

	if needsHostSelection {
		host, metrics, err := selectBestHost()
		if err != nil {
			return false, fmt.Errorf("failed to select host: %w", err)
		}

		cfg.SelectedHost = host
		cfg.LastServerCheck = time.Now()
		maps.Copy(cfg.HostMetrics, metrics)
		configChanged = true
	}

	return configChanged, nil
}
