package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const CrunchyBridgeAPIBaseURL = "https://api.crunchybridge.com"

// CrunchyBridge API docs:
// - https://docs.crunchybridge.com/api/cluster
// - https://docs.crunchybridge.com/api/cluster-backup

type CrunchyBridgeClient struct {
	APIKey  string
	BaseURL string
	client  *http.Client
}

func NewCrunchyBridgeClient(apiKey string) *CrunchyBridgeClient {
	return &CrunchyBridgeClient{
		APIKey:  apiKey,
		BaseURL: CrunchyBridgeAPIBaseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type Cluster struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	ProviderID   string `json:"provider_id"`
	RegionID     string `json:"region_id"`
	MajorVersion int    `json:"major_version"`
	IsHA         bool   `json:"is_ha"`
	TeamID       string `json:"team_id"`
	State        string `json:"state"`
}

type Backup struct {
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	LSNStart   string    `json:"lsn_start"`
	LSNStop    string    `json:"lsn_stop"`
	SizeBytes  int64     `json:"size_bytes"`
}

type BackupToken struct {
	AWS      *AWSConfig   `json:"aws,omitempty"`
	Azure    *AzureConfig `json:"azure,omitempty"`
	GCP      *GCPConfig   `json:"gcp,omitempty"`
	RepoPath string       `json:"repo_path"`
	Type     string       `json:"type"`
	Stanza   string       `json:"stanza"`
}

type CreateClusterRequest struct {
	Name              string `json:"name"`
	PlanID            string `json:"plan_id"`
	TeamID            string `json:"team_id"`
	IsHA              *bool  `json:"is_ha,omitempty"`
	PostgresVersionID *int   `json:"postgres_version_id,omitempty"`
	ProviderID        string `json:"provider_id,omitempty"`
	RegionID          string `json:"region_id,omitempty"`
	Storage           *int   `json:"storage,omitempty"`
}

type AWSConfig struct {
	S3Bucket    string `json:"s3_bucket"`
	S3Key       string `json:"s3_key"`
	S3KeySecret string `json:"s3_key_secret"`
	S3Region    string `json:"s3_region"`
	S3Token     string `json:"s3_token"`
}

type AzureConfig struct {
	StorageAccount string `json:"storage_account"`
	StorageKey     string `json:"storage_key"`
	Container      string `json:"container"`
}

type GCPConfig struct {
	Bucket            string `json:"bucket"`
	ServiceAccountKey string `json:"service_account_key"`
}

type PostgresRole struct {
	ClusterID string `json:"cluster_id"`
	Name      string `json:"name"`
	Flavor    string `json:"flavor"`
	Password  string `json:"password"`
	URI       string `json:"uri"`
	TeamID    string `json:"team_id"`
}

// FindClusterByName finds a cluster by name (case-sensitive)
func (c *CrunchyBridgeClient) FindClusterByName(name string) (*Cluster, error) {
	clusters, err := c.ListClusters()
	if err != nil {
		return nil, err
	}

	for _, cluster := range clusters {
		if cluster.Name == name {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("cluster with name '%s' not found", name)
}

func (c *CrunchyBridgeClient) ListClusters() ([]Cluster, error) {
	var allClusters []Cluster
	cursor := ""

	for {
		params := url.Values{}
		params.Set("limit", "200") // Max allowed
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		url := fmt.Sprintf("%s/clusters?%s", c.BaseURL, params.Encode())

		resp, err := c.makeRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list clusters: %w", err)
		}

		var response struct {
			Clusters   []Cluster `json:"clusters"`
			HasMore    bool      `json:"has_more"`
			NextCursor string    `json:"next_cursor"`
		}

		if err := json.Unmarshal(resp, &response); err != nil {
			return nil, fmt.Errorf("failed to parse clusters response: %w", err)
		}

		allClusters = append(allClusters, response.Clusters...)

		if !response.HasMore {
			break
		}
		cursor = response.NextCursor
	}

	return allClusters, nil
}

// ListBackups returns available backups for a cluster
func (c *CrunchyBridgeClient) ListBackups(clusterID string) ([]Backup, error) {
	var allBackups []Backup
	cursor := ""

	for {
		params := url.Values{}
		params.Set("limit", "200") // Max allowed
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		url := fmt.Sprintf("%s/clusters/%s/backups?%s", c.BaseURL, clusterID, params.Encode())

		resp, err := c.makeRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list backups: %w", err)
		}

		var response struct {
			Backups    []Backup `json:"backups"`
			HasMore    bool     `json:"has_more"`
			NextCursor string   `json:"next_cursor"`
		}

		if err := json.Unmarshal(resp, &response); err != nil {
			return nil, fmt.Errorf("failed to parse backups response: %w", err)
		}

		allBackups = append(allBackups, response.Backups...)

		if !response.HasMore {
			break
		}
		cursor = response.NextCursor
	}

	return allBackups, nil
}

func (c *CrunchyBridgeClient) CreateBackupToken(clusterID string) (*BackupToken, error) {
	url := fmt.Sprintf("%s/clusters/%s/backup-tokens", c.BaseURL, clusterID)

	resp, err := c.makeRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup token: %w", err)
	}

	var token BackupToken
	if err := json.Unmarshal(resp, &token); err != nil {
		return nil, fmt.Errorf("failed to parse backup token response: %w", err)
	}

	return &token, nil
}

// CreateCluster creates a new cluster
func (c *CrunchyBridgeClient) CreateCluster(req CreateClusterRequest) (*Cluster, error) {
	url := fmt.Sprintf("%s/clusters", c.BaseURL)

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create cluster request: %w", err)
	}

	resp, err := c.makeRequest("POST", url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	var cluster Cluster
	if err := json.Unmarshal(resp, &cluster); err != nil {
		return nil, fmt.Errorf("failed to parse create cluster response: %w", err)
	}

	return &cluster, nil
}

// GetCluster retrieves a single cluster by ID
func (c *CrunchyBridgeClient) GetCluster(clusterID string) (*Cluster, error) {
	url := fmt.Sprintf("%s/clusters/%s", c.BaseURL, clusterID)

	resp, err := c.makeRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}

	var cluster Cluster
	if err := json.Unmarshal(resp, &cluster); err != nil {
		return nil, fmt.Errorf("failed to parse cluster response: %w", err)
	}

	return &cluster, nil
}

// DestroyCluster deletes a cluster
func (c *CrunchyBridgeClient) DestroyCluster(clusterID string) error {
	url := fmt.Sprintf("%s/clusters/%s", c.BaseURL, clusterID)

	_, err := c.makeRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to destroy cluster: %w", err)
	}

	return nil
}

// StartBackup initiates a backup for a cluster
func (c *CrunchyBridgeClient) StartBackup(clusterID string) error {
	url := fmt.Sprintf("%s/clusters/%s/actions/start-backup", c.BaseURL, clusterID)

	_, err := c.makeRequest("PUT", url, nil)
	if err != nil {
		return fmt.Errorf("failed to start backup: %w", err)
	}

	return nil
}

// GetRole retrieves a PostgreSQL role with connection details
func (c *CrunchyBridgeClient) GetRole(clusterID, roleName string) (*PostgresRole, error) {
	url := fmt.Sprintf("%s/clusters/%s/roles/%s", c.BaseURL, clusterID, roleName)

	resp, err := c.makeRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get role: %w", err)
	}

	var role PostgresRole
	if err := json.Unmarshal(resp, &role); err != nil {
		return nil, fmt.Errorf("failed to parse role response: %w", err)
	}

	return &role, nil
}

// makeRequest performs HTTP request with authentication
func (c *CrunchyBridgeClient) makeRequest(method, url string, body []byte) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication header
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Quic/1.0")

	// DEBUG: Log the API key being used (first 10 chars for security)
	keyLen := len(c.APIKey)
	if keyLen > 10 {
		keyLen = 10
	}
	fmt.Printf("DEBUG: Making %s request to %s with API key: %s...\n", method, url, c.APIKey[:keyLen])

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// DEBUG: Log response status and body
	fmt.Printf("DEBUG: Response status: %d, body length: %d bytes\n", resp.StatusCode, len(responseBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Printf("DEBUG: Error response body: %s\n", string(responseBody))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	return responseBody, nil
}

// GeneratePgBackRestConfig generates pgBackRest configuration from backup token
func (t *BackupToken) GeneratePgBackRestConfig(stanzaName, pgDataPath string) string {
	var config strings.Builder

	// Global configuration section for all stanzas
	config.WriteString("[global]\n")
	config.WriteString("log-path=/var/log/pgbackrest\n")
	config.WriteString("spool-path=/var/spool/pgbackrest\n")
	config.WriteString("lock-path=/tmp\n")
	config.WriteString("\n")

	config.WriteString(fmt.Sprintf("[%s]\n", stanzaName))
	config.WriteString(fmt.Sprintf("pg1-path=%s\n", pgDataPath))
	config.WriteString(fmt.Sprintf("repo1-path=%s\n", t.RepoPath))

	switch t.Type {
	case "s3":
		if t.AWS != nil {
			config.WriteString("repo1-type=s3\n")
			config.WriteString(fmt.Sprintf("repo1-s3-bucket=%s\n", t.AWS.S3Bucket))
			config.WriteString(fmt.Sprintf("repo1-s3-key=%s\n", t.AWS.S3Key))
			config.WriteString(fmt.Sprintf("repo1-s3-key-secret=%s\n", t.AWS.S3KeySecret))
			config.WriteString(fmt.Sprintf("repo1-s3-region=%s\n", t.AWS.S3Region))
			config.WriteString("repo1-s3-endpoint=s3.amazonaws.com\n")            // CrunchyBridge S3 endpoint
			config.WriteString(fmt.Sprintf("repo1-s3-token=%s\n", t.AWS.S3Token)) // STS session token
		}
	case "azure":
		if t.Azure != nil {
			config.WriteString("repo1-type=azure\n")
			config.WriteString(fmt.Sprintf("repo1-azure-account=%s\n", t.Azure.StorageAccount))
			config.WriteString(fmt.Sprintf("repo1-azure-key=%s\n", t.Azure.StorageKey))
			config.WriteString(fmt.Sprintf("repo1-azure-container=%s\n", t.Azure.Container))
		}
	case "gcs", "gcp":
		if t.GCP != nil {
			config.WriteString("repo1-type=gcs\n")
			config.WriteString(fmt.Sprintf("repo1-gcs-bucket=%s\n", t.GCP.Bucket))
			config.WriteString(fmt.Sprintf("repo1-gcs-key=%s\n", t.GCP.ServiceAccountKey))
		}
	}

	return config.String()
}
