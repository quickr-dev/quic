package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type InitConfig struct {
	Stanza   string
	Database string
	Dirname  string
}

type InitResult struct {
	Dirname     string `json:"dirname"`
	Stanza      string `json:"stanza"`
	Database    string `json:"database"`
	MountPath   string `json:"mount_path"`
	Port        int    `json:"port"`
	ServiceName string `json:"service_name"`
	CreatedAt   string `json:"created_at"`
}

func (s *AgentService) InitRestore(config *InitConfig) (*InitResult, error) {
	datasetPath := fmt.Sprintf("%s/%s", ZPool, config.Dirname)
	mountPath := fmt.Sprintf("/opt/quic/%s/_restore", config.Dirname)

	// Check if directory already exists
	if _, err := os.Stat(mountPath); !os.IsNotExist(err) {
		return nil, fmt.Errorf("mount path %s already exists", mountPath)
	}

	// Create ZFS dataset
	cmd := exec.Command("sudo", "zfs", "create", "-o", fmt.Sprintf("mountpoint=%s", mountPath), datasetPath)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("creating ZFS dataset: %w", err)
	}

	// Perform pgbackrest restore
	if err := exec.Command("sudo", "pgbackrest", "--archive-mode=off", "--stanza="+config.Stanza, "--config=/etc/pgbackrest.conf", "restore", "--type=standby", "--pg1-path="+mountPath).Run(); err != nil {
		return nil, fmt.Errorf("pgbackrest restore: %w", err)
	}

	// Set ownership
	if err := exec.Command("sudo", "chown", "-R", "postgres:postgres", mountPath).Run(); err != nil {
		return nil, fmt.Errorf("setting ownership: %w", err)
	}

	// Find available port
	port, err := findAvailablePortForInit()
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	// Create systemd service
	serviceName := fmt.Sprintf("postgresql-%s", config.Dirname)
	if err := createPostgreSQLSystemdService(config.Dirname, mountPath, port, serviceName); err != nil {
		return nil, fmt.Errorf("creating systemd service: %w", err)
	}

	// Store metadata
	result := &InitResult{
		Dirname:     config.Dirname,
		Stanza:      config.Stanza,
		Database:    config.Database,
		MountPath:   mountPath,
		Port:        port,
		ServiceName: serviceName,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	metadataPath := filepath.Join(mountPath, ".quic-init-meta.json")
	metadataBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling metadata: %w", err)
	}

	cmd = exec.Command("sudo", "tee", metadataPath)
	cmd.Stdin = strings.NewReader(string(metadataBytes))
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("writing metadata: %w", err)
	}

	// Start the PostgreSQL service
	if err := startPostgreSQLService(serviceName); err != nil {
		return nil, fmt.Errorf("starting PostgreSQL service: %w", err)
	}

	// Wait for PostgreSQL to be ready
	if err := waitForPostgreSQLReady(port, 60*time.Second); err != nil {
		return nil, fmt.Errorf("waiting for PostgreSQL to be ready: %w", err)
	}

	return result, nil
}

func findAvailablePortForInit() (int, error) {
	for port := StartPort; port <= EndPort; port++ {
		conn, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			continue
		}
		conn.Close()

		return port, nil
	}

	return 0, fmt.Errorf("no available ports in range %d-%d", StartPort, EndPort)
}

// createPostgreSQLSystemdService creates a systemd service for the restored PostgreSQL instance
func createPostgreSQLSystemdService(dirname, mountPath string, port int, serviceName string) error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=PostgreSQL database server (restored instance - %[1]s)
Documentation=man:postgres(1)
After=network.target zfs-unlock.service

[Service]
Type=forking
User=postgres
ExecStart=%[2]s start -D %[3]s -o "--port=%[4]d" -w -t 300
ExecStop=%[2]s stop -D %[3]s -m fast
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutStartSec=1200
TimeoutStopSec=300
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`, dirname, pgCtlPath(PgVersion), mountPath, port)

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)

	cmd := exec.Command("sudo", "tee", servicePath)
	cmd.Stdin = strings.NewReader(serviceContent)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing systemd service file: %w", err)
	}

	// Reload systemd daemon
	if err := exec.Command("sudo", "systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("reloading systemd daemon: %w", err)
	}

	// Enable the service
	if err := exec.Command("sudo", "systemctl", "enable", serviceName).Run(); err != nil {
		return fmt.Errorf("enabling systemd service: %w", err)
	}

	return nil
}

// startPostgreSQLService starts the PostgreSQL systemd service
func startPostgreSQLService(serviceName string) error {
	if err := exec.Command("sudo", "systemctl", "start", serviceName).Run(); err != nil {
		return fmt.Errorf("starting systemd service %s: %w", serviceName, err)
	}
	return nil
}

// waitForPostgreSQLReady waits for PostgreSQL to accept connections
func waitForPostgreSQLReady(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
		if err == nil {
			conn.Close()
			// Additional check: try to connect with pg_isready
			cmd := exec.Command("sudo", "-u", "postgres", "pg_isready", "-p", fmt.Sprintf("%d", port))
			if cmd.Run() == nil {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("PostgreSQL not ready after %v timeout", timeout)
}
