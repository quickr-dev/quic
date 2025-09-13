package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (s *AgentService) CreateBranch(ctx context.Context, branch string, template string, createdBy string) (*BranchInfo, error) {
	templatePath, err := GetMountpoint(GetTemplateDataset(template))
	if err != nil {
		return nil, err
	}

	if !IsPostgreSQLServerReady(templatePath) {
		return nil, fmt.Errorf("template is still in recovery mode and not ready for branching. This process may take seconds to hours depending on WAL volume. Please retry in a few moments")
	}

	if !s.tryLockWithShutdownCheck() {
		return nil, fmt.Errorf("service restarting, please retry in a few seconds")
	}
	defer s.checkoutMutex.Unlock()

	// Validate and normalize clone name
	validatedName, err := ValidateBranchName(branch)
	if err != nil {
		return nil, fmt.Errorf("invalid clone name: %w", err)
	}
	branch = validatedName

	existing, err := s.getBranchMetadata(GetBranchDataset(template, branch))
	if err != nil {
		return nil, fmt.Errorf("checking existing checkout: %w", err)
	}
	if existing != nil {
		return existing, nil // Already exists
	}

	// Find available port from OS
	port, err := findAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	// Generate admin password
	adminPassword, err := generateSecurePassword()
	if err != nil {
		return nil, fmt.Errorf("generating password: %w", err)
	}

	// Create ZFS snapshot and clone
	clonePath, err := s.createZFSClone(template, branch)
	if err != nil {
		return nil, fmt.Errorf("creating ZFS clone: %w", err)
	}

	// Store metadata alongside the clone
	now := time.Now().UTC().Truncate(time.Second)
	checkout := &BranchInfo{
		TemplateName:  template,
		BranchName:    branch,
		Port:          port,
		BranchPath:    clonePath,
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
	if err := CreateBranchService(checkout.TemplateName, checkout.BranchName, checkout.BranchPath, checkout.Port); err != nil {
		return nil, fmt.Errorf("creating systemd service: %w", err)
	}

	// Start the systemd service
	serviceName := GetBranchServiceName(checkout.TemplateName, checkout.BranchName)
	if err := StartService(serviceName); err != nil {
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

func (s *AgentService) createZFSClone(template, branch string) (string, error) {
	templateDataset := GetTemplateDataset(template)

	// Check if restore dataset exists
	if !datasetExists(templateDataset) {
		return "", fmt.Errorf("restore dataset %s does not exist", templateDataset)
	}

	// ZFS snapshot
	err := s.createBranchSnapshot(template, branch)
	if err != nil {
		return "", fmt.Errorf("creating branch snapshot: %w", err)
	}

	// ZFS clone
	mountpoint, err := s.createBranchClone(template, branch)
	if err != nil {
		return "", fmt.Errorf("getting clone mountpoint: %w", err)
	}

	return mountpoint, nil
}

func (s *AgentService) createBranchClone(template, branch string) (string, error) {
	branchDataset := GetBranchDataset(template, branch)
	mountpoint := GetBranchMountpoint(template, branch)

	if !datasetExists(branchDataset) {
		snapshotName := GetSnapshotName(template, branch)
		err := createClone(snapshotName, branchDataset, mountpoint)
		if err != nil {
			return "", fmt.Errorf("creating branch clone: %w", err)
		}
	}

	return mountpoint, nil
}

func (s *AgentService) createBranchSnapshot(template, branch string) error {
	snapshotName := GetSnapshotName(template, branch)
	if snapshotExists(snapshotName) {
		return nil
	}

	sourcePath, err := GetMountpoint(GetTemplateDataset(template))
	if err != nil {
		return fmt.Errorf("getting mountpoint: %w", err)
	}

	postmasterPid, isRunning := getPostmasterPid(sourcePath)
	if !isRunning {
		// PostgreSQL isn't running, just create snapshot
		return createSnapshot(snapshotName)
	}

	// PostgreSQL is running and ready - force checkpoint before taking snapshot
	if _, err := ExecPostgresCommand(postmasterPid.Port, "postgres", "CHECKPOINT;"); err != nil {
		return fmt.Errorf("forcing checkpoint: %w", err)
	}
	return createSnapshot(snapshotName)
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

func saveCheckoutMetadata(checkout *BranchInfo) error {
	metadataPath := filepath.Join(checkout.BranchPath, ".quic-meta.json")

	metadata := map[string]interface{}{
		"template_name":  checkout.TemplateName,
		"branch_name":    checkout.BranchName,
		"port":           checkout.Port,
		"branch_path":    checkout.BranchPath,
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

func (s *AgentService) setupAdminUser(branch *BranchInfo) error {
	sqlCommands := fmt.Sprintf(`
		DO $$ BEGIN
			CREATE ROLE admin WITH LOGIN SUPERUSER CREATEDB CREATEROLE REPLICATION BYPASSRLS PASSWORD '%s';
		EXCEPTION
			WHEN duplicate_object THEN
				ALTER ROLE admin WITH SUPERUSER CREATEDB CREATEROLE REPLICATION BYPASSRLS PASSWORD '%s';
		END $$;
	`, branch.AdminPassword, branch.AdminPassword)

	_, err := ExecPostgresCommand(branch.Port, "postgres", sqlCommands)
	return err
}

func (s *AgentService) getBranchMetadata(dataset string) (*BranchInfo, error) {
	if !datasetExists(dataset) {
		return nil, nil
	}

	mountpoint, err := GetMountpoint(dataset)
	if err != nil {
		return nil, fmt.Errorf("getting ZFS mountpoint: %w", err)
	}

	var branch *BranchInfo

	if mountpoint != "none" && mountpoint != "-" && mountpoint != "" {
		branch, err = loadBranchMetadata(mountpoint)
		if err != nil {
			return nil, fmt.Errorf("loading branch metadata: %w", err)
		}
	}

	return branch, nil
}

func loadBranchMetadata(branchPath string) (*BranchInfo, error) {
	metadataPath := filepath.Join(branchPath, ".quic-meta.json")

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

	checkout := &BranchInfo{
		TemplateName:  getString(metadata, "template_name"),
		BranchName:    getString(metadata, "branch_name"),
		Port:          getString(metadata, "port"),
		BranchPath:    branchPath,
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

func generateSecurePassword() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}
