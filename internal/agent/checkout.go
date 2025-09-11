package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (s *AgentService) CreateBranch(ctx context.Context, cloneName string, templateName string, createdBy string) (*CheckoutInfo, error) {
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

	existing, err := s.discoverCheckoutFromOS(templateName, cloneName)
	if err != nil {
		return nil, fmt.Errorf("checking existing checkout: %w", err)
	}
	if existing != nil {
		return existing, nil // Already exists
	}

	// Find available port from OS
	port, err := findAvailablePortFromOS()
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	// Generate admin password
	adminPassword, err := generateSecurePassword()
	if err != nil {
		return nil, fmt.Errorf("generating password: %w", err)
	}

	// Create ZFS snapshot and clone
	clonePath, err := s.createZFSClone(templateName, cloneName)
	if err != nil {
		return nil, fmt.Errorf("creating ZFS clone: %w", err)
	}

	// Store metadata alongside the clone
	now := time.Now().UTC().Truncate(time.Second)
	checkout := &CheckoutInfo{
		TemplateName:  templateName,
		CloneName:     cloneName,
		Port:          port,
		ClonePath:     clonePath,
		AdminPassword: adminPassword,
		CreatedBy:     createdBy,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Prepare clone for startup (remove standby config, reset WAL, configure access)
	if err := prepareCloneForStartup(clonePath); err != nil {
		return nil, fmt.Errorf("preparing clone for startup: %w", err)
	}

	// Save metadata to filesystem (after permissions are set)
	if err := saveCheckoutMetadata(checkout); err != nil {
		return nil, fmt.Errorf("saving checkout metadata: %w", err)
	}

	// Create and start systemd service for this clone
	if err := CreateCloneService(checkout.TemplateName, checkout.CloneName, checkout.ClonePath, checkout.Port); err != nil {
		return nil, fmt.Errorf("creating systemd service: %w", err)
	}

	// Start the systemd service
	if err := StartCloneService(checkout.TemplateName, checkout.CloneName, checkout.Port); err != nil {
		return nil, fmt.Errorf("starting systemd service: %w", err)
	}

	// Open firewall port
	if err := openFirewallPort(port); err != nil {
		return nil, fmt.Errorf("opening firewall port: %w", err)
	}

	// Setup admin user
	if err := s.setupAdminUser(checkout); err != nil {
		return nil, fmt.Errorf("setting up admin user: %w", err)
	}

	// Audit checkout creation
	if err := auditEvent("checkout_create", checkout); err != nil {
		return nil, fmt.Errorf("auditing checkout creation: %w", err)
	}

	return checkout, nil
}

func (s *AgentService) createZFSClone(restoreName, cloneName string) (string, error) {
	restoreDataset := restoreDataset(restoreName)
	cloneDataset := cloneDataset(restoreName, cloneName)
	snapshotName := restoreDataset + "@" + cloneName

	// Check if restore dataset exists
	if !datasetExists(restoreDataset) {
		return "", fmt.Errorf("restore dataset %s does not exist", restoreDataset)
	}

	// Check if snapshot already exists, if not create it
	if !snapshotExists(snapshotName) {
		// Coordinate with PostgreSQL for consistent snapshot
		if err := s.coordinatePostgreSQLBackup(restoreDataset, snapshotName); err != nil {
			return "", fmt.Errorf("coordinating PostgreSQL backup: %w", err)
		}

		// Audit ZFS snapshot creation
		auditEvent("zfs_snapshot_create", map[string]string{
			"source_dataset": restoreDataset,
			"snapshot_name":  snapshotName,
		})
	}

	return s.getCloneMountpoint(restoreName, cloneDataset, cloneName)
}

func (s *AgentService) getCloneMountpoint(restoreName, cloneDataset, cloneName string) (string, error) {
	// Check if clone already exists, if not create it
	if !datasetExists(cloneDataset) {
		// Construct snapshot name from clone dataset using the restore dataset
		restoreDataset := restoreDataset(restoreName)
		snapshotName := restoreDataset + "@" + cloneName
		cmd := exec.Command("sudo", "zfs", "clone", snapshotName, cloneDataset)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("creating ZFS clone: %w", err)
		}

		// Set explicit mountpoint for the clone to ensure it's mounted
		expectedMountpoint := "/opt/quic/" + restoreName + "/" + cloneName
		cmd = exec.Command("sudo", "zfs", "set", "mountpoint="+expectedMountpoint, cloneDataset)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("setting ZFS clone mountpoint: %w", err)
		}

		// Audit ZFS clone creation
		auditEvent("zfs_clone_create", map[string]string{
			"source_snapshot": snapshotName,
			"clone_dataset":   cloneDataset,
			"mountpoint":      expectedMountpoint,
		})
	}

	// Get mount point
	cmd := GetMountpoint(cloneDataset)
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

func (s *AgentService) coordinatePostgreSQLBackup(restoreDataset, snapshotName string) error {
	// Get the mount point of the source dataset to find the PostgreSQL data directory
	cmd := exec.Command("zfs", "get", "-H", "-o", "value", "mountpoint", restoreDataset)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("getting source dataset mountpoint: %w", err)
	}
	sourcePath := strings.TrimSpace(string(output))

	// Check if PostgreSQL is running on this data directory
	port, isRunning := extractPortFromPostmasterPid(sourcePath)
	if !isRunning {
		// PostgreSQL isn't running, just create snapshot directly
		cmd := exec.Command("sudo", "zfs", "snapshot", snapshotName)
		return cmd.Run()
	}

	// check if server is fully ready
	if !IsPostgreSQLServerReady(sourcePath) {
		return fmt.Errorf("template is in backup recovery state and not yet ready for branching. Please try again in a few minutes.")
	}

	// PostgreSQL is running and ready - force checkpoint for consistency then take snapshot
	if _, err := ExecPostgresCommand(port, "postgres", "CHECKPOINT;"); err != nil {
		return fmt.Errorf("forcing checkpoint: %w", err)
	}

	cmd = exec.Command("sudo", "zfs", "snapshot", snapshotName)
	return cmd.Run()
}

func prepareCloneForStartup(clonePath string) error {
	// Remove standby.signal file
	standbySignalPath := filepath.Join(clonePath, "standby.signal")
	cmd := exec.Command("sudo", "rm", "-f", standbySignalPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("removing standby.signal: %w", err)
	}

	// Remove recovery.signal file
	recoverySignalPath := filepath.Join(clonePath, "recovery.signal")
	cmd = exec.Command("sudo", "rm", "-f", recoverySignalPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("removing recovery.signal: %w", err)
	}

	// Remove recovery.conf if it exists
	recoveryConfPath := filepath.Join(clonePath, "recovery.conf")
	cmd = exec.Command("sudo", "rm", "-f", recoveryConfPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("removing recovery.conf: %w", err)
	}

	// Remove postmaster.pid file to prevent startup conflicts
	postmasterPidPath := filepath.Join(clonePath, "postmaster.pid")
	cmd = exec.Command("sudo", "rm", "-f", postmasterPidPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("removing postmaster.pid: %w", err)
	}

	// Reset WAL for fast startup (skips recovery entirely)
	resetCmd := exec.Command("sudo", "-u", "postgres", pgResetWalPath(PgVersion), "-f", clonePath)
	if err := resetCmd.Run(); err != nil {
		return fmt.Errorf("resetting WAL for fast startup: %w", err)
	}

	// Clean postgresql.auto.conf and configure for clone
	autoConfPath := filepath.Join(clonePath, "postgresql.auto.conf")
	autoConfig := `# Clone instance
archive_mode = 'off'
restore_command = ''
`
	cmd = exec.Command("sudo", "tee", autoConfPath)
	cmd.Stdin = strings.NewReader(autoConfig)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing postgresql.auto.conf: %w", err)
	}

	// Configure postgresql.conf for clone optimization
	postgresqlConfPath := filepath.Join(clonePath, "postgresql.conf")
	if err := updatePostgreSQLConf(postgresqlConfPath); err != nil {
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
	cmd = exec.Command("sudo", "tee", pgHbaPath)
	cmd.Stdin = strings.NewReader(hbaConfig)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing pg_hba.conf: %w", err)
	}

	return nil
}

func updatePostgreSQLConf(confPath string) error {
	cmd := exec.Command("sudo", "cat", confPath)
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("reading postgresql.conf: %w", err)
	}

	config := string(data)

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

	for setting, value := range cloneSettings {
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

	cmd = exec.Command("sudo", "tee", confPath)
	cmd.Stdin = strings.NewReader(config)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing postgresql.conf: %w", err)
	}

	return nil
}

func saveCheckoutMetadata(checkout *CheckoutInfo) error {
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

	cmd := exec.Command("sudo", "tee", metadataPath)
	cmd.Stdin = strings.NewReader(string(data))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing metadata file: %w", err)
	}

	return nil
}

func (s *AgentService) setupAdminUser(checkout *CheckoutInfo) error {
	sqlCommands := fmt.Sprintf(`
		DO $$ BEGIN
			CREATE ROLE admin WITH LOGIN SUPERUSER CREATEDB CREATEROLE REPLICATION BYPASSRLS PASSWORD '%s';
		EXCEPTION
			WHEN duplicate_object THEN
				ALTER ROLE admin WITH SUPERUSER CREATEDB CREATEROLE REPLICATION BYPASSRLS PASSWORD '%s';
		END $$;
	`, checkout.AdminPassword, checkout.AdminPassword)

	_, err := ExecPostgresCommand(checkout.Port, "postgres", sqlCommands)
	return err
}

func findAvailablePortFromOS() (int, error) {
	for port := StartPort; port <= EndPort; port++ {
		if isPortAvailableForClone(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", StartPort, EndPort)
}

func isPortAvailableForClone(port int) bool {
	conn, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	conn.Close()

	if hasUFWRule(port) {
		return false
	}

	return true
}

func waitForPostgresReady(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cmd := exec.Command(pgIsReadyPath(PgVersion), "-h", "localhost", "-p", fmt.Sprintf("%d", port))
		if err := cmd.Run(); err == nil {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for PostgreSQL to become ready after %v", timeout)
}

func (s *AgentService) discoverCheckoutFromOS(templateName, cloneName string) (*CheckoutInfo, error) {
	cloneDataset := cloneDataset(templateName, cloneName)

	if !datasetExists(cloneDataset) {
		return nil, nil // Clone doesn't exist
	}

	cmd := GetMountpoint(cloneDataset)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting ZFS mountpoint: %w", err)
	}
	clonePath := strings.TrimSpace(string(output))

	var checkout *CheckoutInfo

	// If mountpoint is valid, try to load metadata from filesystem
	if clonePath != "none" && clonePath != "-" && clonePath != "" {
		checkout, err = loadCheckoutMetadata(clonePath, cloneName)
		if err != nil {
			return nil, fmt.Errorf("loading checkout metadata: %w", err)
		}
	}

	// If no metadata found (either invalid mountpoint or missing file),
	// create minimal checkout info for cleanup purposes
	if checkout == nil {
		checkout = &CheckoutInfo{
			TemplateName: templateName,
			CloneName:    cloneName,
			ClonePath:    clonePath, // May be "none" but still useful for identification
			Port:         0,         // Unknown port, firewall cleanup will be skipped
		}
	}

	return checkout, nil
}

func loadCheckoutMetadata(clonePath, cloneName string) (*CheckoutInfo, error) {
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
		TemplateName:  getString(metadata, "template_name"),
		CloneName:     cloneName,
		Port:          getInt(metadata, "port"),
		ClonePath:     clonePath,
		AdminPassword: getString(metadata, "admin_password"),
		CreatedBy:     getString(metadata, "created_by"),
	}

	if createdAtStr := getString(metadata, "created_at"); createdAtStr != "" {
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			checkout.CreatedAt = t.UTC()
		}
	}

	if updatedAtStr := getString(metadata, "updated_at"); updatedAtStr != "" {
		if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			checkout.UpdatedAt = t.UTC()
		}
	}

	return checkout, nil
}

func extractPortFromPostmasterPid(dataDir string) (int, bool) {
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

func copyFile(src, dst string) error {
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
	bytes := make([]byte, 24) // 24 bytes = 32 base64 chars
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}
