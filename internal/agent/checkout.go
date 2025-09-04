package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (s *CheckoutService) CreateCheckout(ctx context.Context, cloneName string, restoreName string, createdBy string) (*CheckoutInfo, error) {
	if !s.tryLockWithShutdownCheck() {
		return nil, fmt.Errorf("service restarting, please retry in a few seconds")
	}
	defer s.checkoutMutex.Unlock()

	// Validate and normalize clone name
	validatedName, err := ValidateCloneName(cloneName)
	if err != nil {
		return nil, fmt.Errorf("invalid clone name: %w", err)
	}
	cloneName = validatedName

	zfsConfig := &ZFSConfig{
		ParentDataset: s.config.ZFSParentDataset,
		RestoreName:   restoreName,
	}

	existing, err := s.discoverCheckoutFromOS(zfsConfig, cloneName)
	if err != nil {
		return nil, fmt.Errorf("checking existing checkout: %w", err)
	}
	if existing != nil {
		return existing, nil // Already exists
	}

	// Find available port from OS
	port, err := s.findAvailablePortFromOS()
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	// Generate admin password
	adminPassword, err := generateSecurePassword()
	if err != nil {
		return nil, fmt.Errorf("generating password: %w", err)
	}

	// Create ZFS snapshot and clone
	clonePath, err := s.createZFSClone(zfsConfig, cloneName)
	if err != nil {
		return nil, fmt.Errorf("creating ZFS clone: %w", err)
	}

	// Store metadata alongside the clone
	now := time.Now().Truncate(time.Second)
	checkout := &CheckoutInfo{
		CloneName:     cloneName,
		Port:          port,
		ClonePath:     clonePath,
		AdminPassword: adminPassword,
		CreatedBy:     createdBy,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Set permissions first
	if err := s.setPostgresPermissions(clonePath); err != nil {
		return nil, fmt.Errorf("setting permissions: %w", err)
	}

	// Prepare clone for startup (remove standby config, reset WAL, configure access)
	if err := s.prepareCloneForStartup(clonePath); err != nil {
		return nil, fmt.Errorf("preparing clone for startup: %w", err)
	}

	// Save metadata to filesystem (after permissions are set)
	if err := s.saveCheckoutMetadata(checkout); err != nil {
		return nil, fmt.Errorf("saving checkout metadata: %w", err)
	}

	// Create and start systemd service for this clone
	if err := s.createSystemdService(checkout); err != nil {
		return nil, fmt.Errorf("creating systemd service: %w", err)
	}

	// Start the systemd service
	if err := s.startSystemdService(checkout); err != nil {
		return nil, fmt.Errorf("starting systemd service: %w", err)
	}

	// Open firewall port
	if err := s.openFirewallPort(port); err != nil {
		return nil, fmt.Errorf("opening firewall port: %w", err)
	}

	// Setup admin user
	if err := s.setupAdminUser(checkout); err != nil {
		return nil, fmt.Errorf("setting up admin user: %w", err)
	}

	// Audit checkout creation
	if err := s.auditEvent("checkout_create", checkout); err != nil {
		return nil, fmt.Errorf("auditing checkout creation: %w", err)
	}

	return checkout, nil
}

func (s *CheckoutService) createZFSClone(config *ZFSConfig, cloneName string) (string, error) {
	restoreDataset := config.RestoreDataset()
	cloneDataset := config.CloneDataset(cloneName)
	snapshotName := restoreDataset + "@" + cloneName

	// Check if restore dataset exists
	if !s.datasetExists(restoreDataset) {
		return "", fmt.Errorf("restore dataset %s does not exist", restoreDataset)
	}

	// Check if snapshot already exists, if not create it
	if !s.snapshotExists(snapshotName) {
		// Coordinate with PostgreSQL for consistent snapshot
		if err := s.coordinatePostgreSQLBackup(restoreDataset, snapshotName); err != nil {
			return "", fmt.Errorf("coordinating PostgreSQL backup: %w", err)
		}

		// Audit ZFS snapshot creation
		s.auditEvent("zfs_snapshot_create", map[string]string{
			"source_dataset": restoreDataset,
			"snapshot_name":  snapshotName,
		})
	}

	return s.getCloneMountpoint(config, cloneDataset, cloneName)
}

func (s *CheckoutService) getCloneMountpoint(config *ZFSConfig, cloneDataset, cloneName string) (string, error) {
	// Check if clone already exists, if not create it
	if !s.datasetExists(cloneDataset) {
		// Construct snapshot name from clone dataset using the restore dataset
		restoreDataset := config.RestoreDataset()
		snapshotName := restoreDataset + "@" + cloneName
		cmd := exec.Command("sudo", "zfs", "clone", snapshotName, cloneDataset)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("creating ZFS clone: %w", err)
		}

		// Set explicit mountpoint for the clone to ensure it's mounted
		expectedMountpoint := "/opt/quic/" + config.RestoreName + "/" + cloneName
		cmd = exec.Command("sudo", "zfs", "set", "mountpoint="+expectedMountpoint, cloneDataset)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("setting ZFS clone mountpoint: %w", err)
		}

		// Audit ZFS clone creation
		s.auditEvent("zfs_clone_create", map[string]string{
			"source_snapshot": snapshotName,
			"clone_dataset":   cloneDataset,
			"mountpoint":      expectedMountpoint,
		})
	}

	// Get mount point
	cmd := exec.Command("sudo", "zfs", "get", "-H", "-o", "value", "mountpoint", cloneDataset)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting ZFS mountpoint: %w", err)
	}

	mountpoint := strings.TrimSpace(string(output))

	// Validate that we got a real path, not "none" or other invalid values
	if mountpoint == "none" || mountpoint == "-" || mountpoint == "" {
		return "", fmt.Errorf("invalid ZFS mountpoint for clone %s: got '%s'", cloneDataset, mountpoint)
	}

	return mountpoint, nil
}

func (s *CheckoutService) coordinatePostgreSQLBackup(restoreDataset, snapshotName string) error {
	// Get the mount point of the source dataset to find the PostgreSQL data directory
	cmd := exec.Command("zfs", "get", "-H", "-o", "value", "mountpoint", restoreDataset)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("getting source dataset mountpoint: %w", err)
	}
	sourcePath := strings.TrimSpace(string(output))

	// Check if PostgreSQL is running on this data directory
	port, isRunning := s.extractPortFromPostmasterPid(sourcePath)
	if !isRunning {
		// PostgreSQL isn't running, just create snapshot directly
		cmd := exec.Command("sudo", "zfs", "snapshot", snapshotName)
		return cmd.Run()
	}

	// PostgreSQL is running - force checkpoint for consistency then take snapshot
	if _, err := s.ExecPostgresCommand(port, "postgres", "CHECKPOINT;"); err != nil {
		return fmt.Errorf("forcing checkpoint: %w", err)
	}

	cmd = exec.Command("sudo", "zfs", "snapshot", snapshotName)
	return cmd.Run()
}

func (s *CheckoutService) prepareCloneForStartup(clonePath string) error {
	// Remove standby.signal file
	standbySignalPath := filepath.Join(clonePath, "standby.signal")
	if err := os.Remove(standbySignalPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing standby.signal: %w", err)
	}

	// Remove recovery.signal file
	recoverySignalPath := filepath.Join(clonePath, "recovery.signal")
	if err := os.Remove(recoverySignalPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing recovery.signal: %w", err)
	}

	// Remove recovery.conf if it exists
	recoveryConfPath := filepath.Join(clonePath, "recovery.conf")
	if err := os.Remove(recoveryConfPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing recovery.conf: %w", err)
	}

	// Remove postmaster.pid file to prevent startup conflicts
	postmasterPidPath := filepath.Join(clonePath, "postmaster.pid")
	if err := os.Remove(postmasterPidPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing postmaster.pid: %w", err)
	}

	// Reset WAL for fast startup (skips recovery entirely)
	resetCmd := exec.Command("sudo", "-u", "postgres", "/usr/lib/postgresql/16/bin/pg_resetwal", "-f", clonePath)
	if err := resetCmd.Run(); err != nil {
		return fmt.Errorf("resetting WAL for fast startup: %w", err)
	}

	// Clean postgresql.auto.conf and configure for clone
	autoConfPath := filepath.Join(clonePath, "postgresql.auto.conf")
	autoConfig := `# Clone instance
archive_mode = 'off'
restore_command = ''
`
	if err := os.WriteFile(autoConfPath, []byte(autoConfig), 0640); err != nil {
		return fmt.Errorf("writing postgresql.auto.conf: %w", err)
	}

	// Configure postgresql.conf for clone optimization
	postgresqlConfPath := filepath.Join(clonePath, "postgresql.conf")
	if err := s.updatePostgreSQLConf(postgresqlConfPath); err != nil {
		return fmt.Errorf("updating postgresql.conf: %w", err)
	}

	// Configure pg_hba.conf to allow admin user access
	pgHbaPath := filepath.Join(clonePath, "pg_hba.conf")
	hbaConfig := `# Allow local connections for testing
local   all             postgres                                peer
local   all             all                                     md5
host    all             all             127.0.0.1/32            md5
host    all             all             ::1/128                 md5
host    all             admin           0.0.0.0/0               md5
`
	if err := os.WriteFile(pgHbaPath, []byte(hbaConfig), 0640); err != nil {
		return fmt.Errorf("writing pg_hba.conf: %w", err)
	}

	return nil
}

func (s *CheckoutService) updatePostgreSQLConf(confPath string) error {
	// Read existing config
	data, err := os.ReadFile(confPath)
	if err != nil {
		return fmt.Errorf("reading postgresql.conf: %w", err)
	}

	config := string(data)

	// Define clone-specific settings
	// Try to balance good enough performance and resource utilization to support many running instances
	cloneSettings := map[string]string{
		"max_connections":                 "5",
		"wal_level":                       "minimal",
		"max_wal_senders":                 "0",
		"archive_mode":                    "off",
		"max_wal_size":                    "'64MB'",
		"maintenance_work_mem":            "'64MB'",
		"effective_cache_size":            "'256MB'",
		"shared_buffers":                  "'128MB'",
		"work_mem":                        "'256MB'",
		"random_page_cost":                "1.1",
		"max_worker_processes":            "4",
		"max_parallel_workers":            "2",
		"max_parallel_workers_per_gather": "2",
		"synchronous_commit":              "off",
		"listen_addresses":                "'*'",
		"shared_preload_libraries":        "''",
		"ssl":                             "on",
		"ssl_cert_file":                   "'/etc/quic/certs/server.crt'",
		"ssl_key_file":                    "'/etc/quic/certs/server.key'",
		"ssl_ca_file":                     "''",
		"autovacuum":                      "off",
	}

	// Update or add each setting
	for setting, value := range cloneSettings {
		// Check if setting exists and replace it, or add if missing
		settingPattern := fmt.Sprintf("%s = ", setting)
		lines := strings.Split(config, "\n")
		found := false

		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, settingPattern) && !strings.HasPrefix(trimmed, "#") {
				lines[i] = fmt.Sprintf("%s = %s", setting, value)
				found = true
				break
			}
		}

		if !found {
			// Add the setting at the end
			lines = append(lines, fmt.Sprintf("%s = %s", setting, value))
		}

		config = strings.Join(lines, "\n")
	}

	// Write updated config
	if err := os.WriteFile(confPath, []byte(config), 0640); err != nil {
		return fmt.Errorf("writing postgresql.conf: %w", err)
	}

	return nil
}

func (s *CheckoutService) saveCheckoutMetadata(checkout *CheckoutInfo) error {
	metadataPath := filepath.Join(checkout.ClonePath, ".quic-meta.json")

	metadata := map[string]interface{}{
		"clone_name":     checkout.CloneName,
		"port":           checkout.Port,
		"clone_path":     checkout.ClonePath,
		"admin_password": checkout.AdminPassword,
		"created_by":     checkout.CreatedBy,
		"created_at":     checkout.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":     checkout.UpdatedAt.UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	// Write metadata file (directory is owned by postgres)
	if err := os.WriteFile(metadataPath, data, 0640); err != nil {
		return fmt.Errorf("writing metadata file: %w", err)
	}

	// Ownership should already be correct since we're running as postgres

	return nil
}

func (s *CheckoutService) createSystemdService(checkout *CheckoutInfo) error {
	serviceName := fmt.Sprintf("quic-clone-%s", checkout.CloneName)
	serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)

	serviceContent := fmt.Sprintf(`[Unit]
Description=Quic PostgreSQL Clone (%s)
Documentation=https://github.com/quickr-dev/quic
After=network.target

[Service]
Type=forking
User=postgres
ExecStart=/usr/lib/postgresql/16/bin/pg_ctl start -D %s -o "--port=%d" -w -t 300
ExecStop=/usr/lib/postgresql/16/bin/pg_ctl stop -D %s -m fast
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutStartSec=300
TimeoutStopSec=300
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, checkout.CloneName, checkout.ClonePath, checkout.Port, checkout.ClonePath)

	// Write service file using sudo tee (safer than bash)
	cmd := exec.Command("sudo", "tee", serviceFilePath)
	cmd.Stdin = strings.NewReader(serviceContent)
	cmd.Stdout = nil // Discard tee output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing systemd service file: %w", err)
	}

	// Reload systemd daemon
	cmd = exec.Command("sudo", "systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reloading systemd daemon: %w", err)
	}

	// Enable service for auto-start on boot
	cmd = exec.Command("sudo", "systemctl", "enable", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("enabling systemd service: %w", err)
	}

	return nil
}

func (s *CheckoutService) startSystemdService(checkout *CheckoutInfo) error {
	serviceName := fmt.Sprintf("quic-clone-%s", checkout.CloneName)

	// Start the service
	cmd := exec.Command("sudo", "systemctl", "start", serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Get service status for debugging
		statusCmd := exec.Command("sudo", "systemctl", "status", serviceName)
		statusOutput, _ := statusCmd.CombinedOutput()

		// Get journalctl logs for more details
		logsCmd := exec.Command("sudo", "journalctl", "-u", serviceName, "--no-pager", "-n", "20")
		logsOutput, _ := logsCmd.CombinedOutput()

		return fmt.Errorf("starting systemd service: %w (start output: %s) (status: %s) (logs: %s)",
			err, string(output), string(statusOutput), string(logsOutput))
	}

	// Wait for PostgreSQL to be ready
	if err := s.waitForPostgresReady(checkout.Port, 30*time.Second); err != nil {
		return fmt.Errorf("PostgreSQL failed to become ready on port %d: %w", checkout.Port, err)
	}

	// Audit service start
	s.auditEvent("systemd_service_start", map[string]interface{}{
		"clone_name":   checkout.CloneName,
		"service_name": serviceName,
		"port":         checkout.Port,
	})

	return nil
}

func (s *CheckoutService) stopSystemdService(checkout *CheckoutInfo) error {
	serviceName := fmt.Sprintf("quic-clone-%s", checkout.CloneName)

	// Stop the service
	cmd := exec.Command("sudo", "systemctl", "stop", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stopping systemd service: %w", err)
	}

	return nil
}

func (s *CheckoutService) removeSystemdService(checkout *CheckoutInfo) error {
	serviceName := fmt.Sprintf("quic-clone-%s", checkout.CloneName)
	serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)

	// Stop and disable the service first
	exec.Command("sudo", "systemctl", "stop", serviceName).Run()
	exec.Command("sudo", "systemctl", "disable", serviceName).Run()

	// Remove the service file
	cmd := exec.Command("sudo", "rm", "-f", serviceFilePath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("removing systemd service file: %w", err)
	}

	// Reload systemd daemon
	cmd = exec.Command("sudo", "systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reloading systemd daemon after removal: %w", err)
	}

	return nil
}

func (s *CheckoutService) setupAdminUser(checkout *CheckoutInfo) error {
	// Connect to the database and set up admin user using Unix socket (more reliable for postgres user)
	sqlCommands := fmt.Sprintf(`
		-- Create admin role if it doesn't exist
		DO $$ BEGIN
			CREATE ROLE admin WITH LOGIN SUPERUSER PASSWORD '%s';
		EXCEPTION
			WHEN duplicate_object THEN
				ALTER ROLE admin WITH PASSWORD '%s';
		END $$;
		-- Grant all privileges
		GRANT ALL PRIVILEGES ON DATABASE postgres TO admin;
	`, checkout.AdminPassword, checkout.AdminPassword)

	_, err := s.ExecPostgresCommand(checkout.Port, "postgres", sqlCommands)
	return err
}

func (s *CheckoutService) findAvailablePortFromOS() (int, error) {
	for port := s.config.StartPort; port <= s.config.EndPort; port++ {
		if s.isPortAvailableForClone(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", s.config.StartPort, s.config.EndPort)
}

func (s *CheckoutService) isPortAvailableForClone(port int) bool {
	// Check if port is actually in use on the OS
	conn, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false // Port is in use or unavailable
	}
	conn.Close()

	// Also check if UFW already has a rule for this port
	if s.hasUFWRule(port) {
		return false // Port has existing firewall rule
	}

	return true
}

func (s *CheckoutService) isPostgresListening(port int) bool {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (s *CheckoutService) waitForPostgresReady(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pgIsReadyCmd := "/usr/lib/postgresql/16/bin/pg_isready"
		cmd := exec.Command(pgIsReadyCmd, "-h", "localhost", "-p", fmt.Sprintf("%d", port))
		if err := cmd.Run(); err == nil {
			return nil // PostgreSQL is ready to accept connections
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for PostgreSQL to become ready after %v", timeout)
}

func (s *CheckoutService) discoverCheckoutFromOS(zfsConfig *ZFSConfig, cloneName string) (*CheckoutInfo, error) {
	cloneDataset := zfsConfig.CloneDataset(cloneName)

	// Check if ZFS clone exists
	if !s.datasetExists(cloneDataset) {
		return nil, nil // Clone doesn't exist
	}

	// Get mount point
	cmd := exec.Command("sudo", "zfs", "get", "-H", "-o", "value", "mountpoint", cloneDataset)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting ZFS mountpoint: %w", err)
	}
	clonePath := strings.TrimSpace(string(output))

	var checkout *CheckoutInfo

	// If mountpoint is valid, try to load metadata from filesystem
	if clonePath != "none" && clonePath != "-" && clonePath != "" {
		checkout, err = s.loadCheckoutMetadata(clonePath, cloneName)
		if err != nil {
			return nil, fmt.Errorf("loading checkout metadata: %w", err)
		}
	}

	// If no metadata found (either invalid mountpoint or missing file),
	// create minimal checkout info for cleanup purposes
	if checkout == nil {
		checkout = &CheckoutInfo{
			CloneName: cloneName,
			ClonePath: clonePath, // May be "none" but still useful for identification
			Port:      0,         // Unknown port, firewall cleanup will be skipped
		}
	}

	return checkout, nil
}

func (s *CheckoutService) loadCheckoutMetadata(clonePath, cloneName string) (*CheckoutInfo, error) {
	metadataPath := filepath.Join(clonePath, ".quic-meta.json")

	// Read metadata file directly since agent runs as postgres user
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No metadata file indicates incomplete checkout creation
			return nil, nil
		}
		return nil, fmt.Errorf("reading metadata file: %w", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("unmarshaling metadata: %w", err)
	}

	checkout := &CheckoutInfo{
		CloneName:     cloneName, // Derived from function parameter
		Port:          getInt(metadata, "port"),
		ClonePath:     clonePath, // Derived from function parameter
		AdminPassword: getString(metadata, "admin_password"),
		CreatedBy:     getString(metadata, "created_by"),
	}

	if createdAtStr := getString(metadata, "created_at"); createdAtStr != "" {
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			checkout.CreatedAt = t
		}
	}

	if updatedAtStr := getString(metadata, "updated_at"); updatedAtStr != "" {
		if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			checkout.UpdatedAt = t
		}
	}

	return checkout, nil
}

func (s *CheckoutService) extractPortFromPostmasterPid(dataDir string) (int, bool) {
	postmasterPidPath := filepath.Join(dataDir, "postmaster.pid")
	data, err := os.ReadFile(postmasterPidPath)
	if err != nil {
		return 0, false // Can't read postmaster.pid
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 4 {
		return 0, false // postmaster.pid doesn't have expected format
	}

	// Fourth line contains the port number
	portLine := strings.TrimSpace(lines[3])
	var port int
	if p, err := fmt.Sscanf(portLine, "%d", &port); p == 1 && err == nil {
		return port, true
	}

	return 0, false // Couldn't parse port
}

func (s *CheckoutService) copyFile(src, dst string) error {
	// Read source file
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading source file %s: %w", src, err)
	}

	// Write to destination
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("writing destination file %s: %w", dst, err)
	}

	return nil
}

func generateSecurePassword() (string, error) {
	// Use alphanumeric characters only for easy selection and URL safety
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 32

	password := make([]byte, length)
	for i := range password {
		randomByte := make([]byte, 1)
		if _, err := rand.Read(randomByte); err != nil {
			return "", err
		}
		password[i] = charset[int(randomByte[0])%len(charset)]
	}

	return string(password), nil
}
