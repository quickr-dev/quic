package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pb "github.com/quickr-dev/quic/proto"
)

type InitResult struct {
	Dirname     string `json:"dirname"`
	Stanza      string `json:"stanza"`
	Database    string `json:"database"`
	MountPath   string `json:"mount_path"`
	Port        string `json:"port"`
	ServiceName string `json:"service_name"`
	CreatedAt   string `json:"created_at"`
}

func (s *AgentService) TemplateSetup(req *pb.RestoreTemplateRequest, stream pb.QuicService_RestoreTemplateServer) error {
	s.sendLog(stream, "INFO", "Starting template restore process...")

	// Create pgbackrest config file
	if err := s.writePgBackRestConfig(req.PgbackrestConfig); err != nil {
		s.sendError(stream, "pgbackrest_config", fmt.Sprintf("Failed to write pgbackrest config: %v", err))
		return err
	}

	s.sendLog(stream, "INFO", "✓ pgBackRest configuration written")

	result, err := s.initRestoreWithStreaming(req, stream)
	if err != nil {
		s.sendError(stream, "restore", fmt.Sprintf("Template restore failed: %v", err))
		return err
	}

	// Send success result
	if err := stream.Send(&pb.RestoreTemplateResponse{
		Message: &pb.RestoreTemplateResponse_Result{
			Result: &pb.RestoreResult{
				TemplateName:     req.TemplateName,
				ConnectionString: fmt.Sprintf("postgresql://postgres@localhost:%s/%s", result.Port, req.Database),
				MountPath:        result.MountPath,
				Port:             result.Port,
				ServiceName:      result.ServiceName,
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to send result: %w", err)
	}

	return nil
}

func (s *AgentService) writePgBackRestConfig(configContent string) error {
	cmd := exec.Command("sudo", "tee", "/etc/pgbackrest.conf")
	cmd.Stdin = strings.NewReader(configContent)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write pgbackrest config: %w", err)
	}

	return nil
}

func (s *AgentService) initRestoreWithStreaming(req *pb.RestoreTemplateRequest, stream pb.QuicService_RestoreTemplateServer) (*InitResult, error) {
	datasetPath := fmt.Sprintf("%s/%s", ZPool, req.TemplateName)
	mountPath := fmt.Sprintf("/opt/quic/%s/_restore", req.TemplateName)

	s.sendLog(stream, "INFO", "Preparing to restore")

	// Check if directory already exists
	if _, err := os.Stat(mountPath); !os.IsNotExist(err) {
		return nil, fmt.Errorf("mount path %s already exists", mountPath)
	}

	// Create ZFS dataset
	cmd := exec.Command("sudo", "zfs", "create", "-o", fmt.Sprintf("mountpoint=%s", mountPath), datasetPath)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("creating ZFS dataset: %w", err)
	}

	// Perform pgbackrest restore with streaming output
	s.sendLog(stream, "INFO", "Starting restore...")

	if err := s.runPgBackRestWithStreaming(req.BackupToken.Stanza, mountPath, stream); err != nil {
		return nil, fmt.Errorf("pgbackrest restore: %w", err)
	}

	s.sendLog(stream, "INFO", "✓ Restore done")
	s.sendLog(stream, "INFO", "Setting up template...")

	// Set ownership
	if err := exec.Command("sudo", "chown", "-R", "postgres:postgres", mountPath).Run(); err != nil {
		return nil, fmt.Errorf("setting ownership: %w", err)
	}

	// Clean up PostgreSQL configuration
	if err := s.updateTemplatePostgresConf(mountPath); err != nil {
		return nil, fmt.Errorf("updating PostgreSQL config: %w", err)
	}

	// Find available port
	port, err := findAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	// Create systemd service
	serviceName := GetTemplateServiceName(req.TemplateName)

	if err := CreateTemplateService(req.TemplateName, mountPath, port); err != nil {
		return nil, fmt.Errorf("creating systemd service: %w", err)
	}

	// Start service
	if err := StartService(serviceName); err != nil {
		return nil, fmt.Errorf("starting PostgreSQL service: %w", err)
	}

	// Store metadata
	result := &InitResult{
		Dirname:     req.TemplateName,
		Stanza:      req.BackupToken.Stanza,
		Database:    req.Database,
		MountPath:   mountPath,
		Port:        port,
		ServiceName: serviceName,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	if err := s.writeMetadataFile(result, mountPath); err != nil {
		return nil, fmt.Errorf("writing metadata file: %w", err)
	}

	templatePath, err := GetMountpoint(GetTemplateDataset(req.TemplateName))
	if err != nil {
		return nil, fmt.Errorf("getting template path: %w", err)
	}

	if IsPostgreSQLServerReady(templatePath) {
		s.sendLog(stream, "INFO", "Template setup complete but not yet ready for branching. For now, you should keep trying to `quic checkout` until it succeeds.")
	} else {
		s.sendLog(stream, "INFO", "✓ Template ready for branching")
	}

	return result, nil
}

func (s *AgentService) runPgBackRestWithStreaming(stanza, pgDataPath string, stream pb.QuicService_RestoreTemplateServer) error {
	cmd := exec.Command("sudo", "pgbackrest",
		"restore",
		"--archive-mode=off",
		"--stanza="+stanza,
		"--config=/etc/pgbackrest.conf",
		"--log-level-console=detail",
		"--log-level-stderr=detail",
		"--type=standby",
		"--pg1-path="+pgDataPath)

	// Get stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start pgbackrest: %w", err)
	}

	// Use WaitGroup to synchronize goroutines
	var wg sync.WaitGroup
	done := make(chan bool)

	// Stream stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			s.sendLog(stream, "INFO", fmt.Sprintf("pgBackRest: %s", line))
		}
	}()

	// Stream stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			s.sendLog(stream, "WARN", fmt.Sprintf("pgBackRest: %s", line))
		}
	}()

	// Send periodic heartbeat messages while the command is running
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				s.sendLog(stream, "INFO", "pgBackRest restore in progress...")
			}
		}
	}()

	// Wait for command to complete
	cmdErr := cmd.Wait()
	close(done) // Signal heartbeat goroutine to stop

	// Wait for all goroutines to finish reading pipes
	wg.Wait()

	if cmdErr != nil {
		return fmt.Errorf("pgbackrest command failed: %w", cmdErr)
	}

	return nil
}

func (s *AgentService) sendLog(stream pb.QuicService_RestoreTemplateServer, level, message string) {
	stream.Send(&pb.RestoreTemplateResponse{
		Message: &pb.RestoreTemplateResponse_Log{
			Log: &pb.LogLine{
				Line:      message,
				Level:     level,
				Timestamp: time.Now().Unix(),
			},
		},
	})
}

func (s *AgentService) sendError(stream pb.QuicService_RestoreTemplateServer, step, message string) {
	stream.Send(&pb.RestoreTemplateResponse{
		Message: &pb.RestoreTemplateResponse_Error{
			Error: &pb.RestoreError{
				ErrorMessage: message,
				Step:         step,
			},
		},
	})
}

func (s *AgentService) updateTemplatePostgresConf(mountPath string) error {
	confPath := fmt.Sprintf("%s/postgresql.conf", mountPath)

	// Read existing config
	cmd := exec.Command("sudo", "cat", confPath)
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("reading postgresql.conf: %w", err)
	}

	config := string(data)

	// Define template-specific settings to clean up CrunchyBridge config
	templateSettings := map[string]string{
		"shared_preload_libraries": "''", // Remove pgaudit and other extensions
		"listen_addresses":         "'127.0.0.1'",
		"ssl":                      "on",
		"ssl_cert_file":            "'/etc/quic/certs/server.crt'",
		"ssl_key_file":             "'/etc/quic/certs/server.key'",
		"ssl_ca_file":              "''",
	}

	// Update or add each setting
	for setting, value := range templateSettings {
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

	// Comment out include_dir
	lines := strings.Split(config, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if (strings.HasPrefix(trimmed, "include_dir = ") || strings.HasPrefix(trimmed, "include_dir ")) && !strings.HasPrefix(trimmed, "#") {
			lines[i] = "# " + line + " # Disabled by Quic template setup"
		}
	}
	config = strings.Join(lines, "\n")

	// Write updated config using sudo
	cmd = exec.Command("sudo", "tee", confPath)
	cmd.Stdin = strings.NewReader(config)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing postgresql.conf: %w", err)
	}

	return nil
}

func (s *AgentService) writeMetadataFile(result *InitResult, mountPath string) error {
	metadataPath := filepath.Join(mountPath, ".quic-init-meta.json")
	metadataBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	cmd := exec.Command("sudo", "tee", metadataPath)
	cmd.Stdin = strings.NewReader(string(metadataBytes))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

func findAvailablePort() (string, error) {
	for port := StartPort; port <= EndPort; port++ {
		conn, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			continue
		}
		conn.Close()

		portStr := fmt.Sprintf("%d", port)
		// Just in case a branch instance is down but it will need the port
		if hasUFWRule(portStr) {
			continue
		}

		return portStr, nil
	}

	return "0", fmt.Errorf("no available ports in range %d-%d", StartPort, EndPort)
}

func isPortAvailableForClone(port string) bool {
	conn, err := net.Listen("tcp", port)
	if err != nil {
		return false
	}
	conn.Close()

	if hasUFWRule(port) {
		return false
	}

	return true
}
