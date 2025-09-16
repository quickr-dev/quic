package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type UserConfig struct {
	AuthToken        string `json:"authToken"`
	SelectedHost     string `json:"selectedHost"`
	SelectedTemplate string `json:"selectedTemplate,omitempty"`
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

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config := createDefaultConfig()
		if err := config.save(); err != nil {
			return nil, fmt.Errorf("failed to save default config: %w", err)
		}
		return config, nil
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

func (c *UserConfig) SetAuthToken(token string) error {
	c.AuthToken = token
	return c.save()
}

func (c *UserConfig) SetSelectedHost(host string) error {
	c.SelectedHost = host
	return c.save()
}

func (c *UserConfig) SetSelectedTemplate(template string) error {
	c.SelectedTemplate = template
	return c.save()
}

func (c *UserConfig) save() error {
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

func createDefaultConfig() *UserConfig {
	config := &UserConfig{}

	projectConfig, err := LoadProjectConfig()
	if err != nil {
		return config
	}

	if len(projectConfig.Hosts) > 0 {
		config.SelectedHost = projectConfig.Hosts[0].IP
	}

	if len(projectConfig.Templates) > 0 {
		config.SelectedTemplate = projectConfig.Templates[0].Name
	}

	return config
}
