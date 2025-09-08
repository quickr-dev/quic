package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	QuicConfigFileName = "quic.json"
	QuicSchemaURL      = "https://quic.dev/0.1.0/schema.json"
)

type QuicConfig struct {
	Schema    string     `json:"$schema"`
	Hosts     []QuicHost `json:"hosts"`
	Templates []Template `json:"templates"`
}

type QuicHost struct {
	IP               string   `json:"ip"`
	Alias            string   `json:"alias"`
	EncryptionAtRest string   `json:"encryptionAtRest"`
	Devices          []string `json:"devices"`
}

type Template struct {
	Name      string           `json:"name"`
	PGVersion string           `json:"pgVersion"`
	Database  string           `json:"database"`
	Provider  TemplateProvider `json:"provider"`
}

type TemplateProvider struct {
	Name        string `json:"name"`
	ClusterName string `json:"clusterName"`
}

func LoadQuicConfig() (*QuicConfig, error) {
	configPath := getQuicConfigPath()

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return createDefaultQuicConfig()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read quic.json: %w", err)
	}

	var config QuicConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse quic.json: %w", err)
	}

	return &config, nil
}

func (c *QuicConfig) Save() error {
	configPath := getQuicConfigPath()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quic.json: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write quic.json: %w", err)
	}

	return nil
}

func (c *QuicConfig) AddHost(host QuicHost) error {
	if err := c.validateHost(host); err != nil {
		return err
	}

	c.Hosts = append(c.Hosts, host)
	return nil
}

func (c *QuicConfig) AddTemplate(template Template) error {
	if err := c.validateTemplate(template); err != nil {
		return err
	}

	c.Templates = append(c.Templates, template)
	return nil
}

func (c *QuicConfig) validateHost(host QuicHost) error {
	if host.IP == "" {
		return fmt.Errorf("host IP cannot be empty")
	}

	if host.Alias == "" {
		return fmt.Errorf("host alias cannot be empty")
	}

	if len(host.Devices) == 0 {
		return fmt.Errorf("host must have at least one device")
	}

	// Check for duplicate IPs
	for _, existingHost := range c.Hosts {
		if existingHost.IP == host.IP {
			return fmt.Errorf("host with IP %s already exists", host.IP)
		}
	}

	// Check for duplicate aliases
	for _, existingHost := range c.Hosts {
		if existingHost.Alias == host.Alias {
			return fmt.Errorf("host with alias %s already exists", host.Alias)
		}
	}

	return nil
}

func (c *QuicConfig) validateTemplate(template Template) error {
	if template.Name == "" {
		return fmt.Errorf("template name cannot be empty")
	}

	if template.PGVersion == "" {
		return fmt.Errorf("template PostgreSQL version cannot be empty")
	}

	if template.Database == "" {
		return fmt.Errorf("template database cannot be empty")
	}

	if template.Provider.Name == "" {
		return fmt.Errorf("template provider name cannot be empty")
	}

	if template.Provider.ClusterName == "" {
		return fmt.Errorf("template provider cluster name cannot be empty")
	}

	// Check for duplicate template names
	for _, existingTemplate := range c.Templates {
		if existingTemplate.Name == template.Name {
			return fmt.Errorf("template with name %s already exists", template.Name)
		}
	}

	return nil
}

func getQuicConfigPath() string {
	return filepath.Join(".", QuicConfigFileName)
}

func createDefaultQuicConfig() (*QuicConfig, error) {
	fmt.Println("Initializing quic.json")

	config := &QuicConfig{
		Schema:    QuicSchemaURL,
		Hosts:     []QuicHost{},
		Templates: []Template{},
	}

	return config, nil
}
