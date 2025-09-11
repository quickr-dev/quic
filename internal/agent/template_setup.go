package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/quickr-dev/quic/proto"
)

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
				ConnectionString: fmt.Sprintf("postgresql://postgres@localhost:%d/%s", result.Port, req.Database),
				MountPath:        result.MountPath,
				Port:             int32(result.Port),
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
	s.sendLog(stream, "INFO", "Setting up the system...")

	// Set ownership
	if err := exec.Command("sudo", "chown", "-R", "postgres:postgres", mountPath).Run(); err != nil {
		return nil, fmt.Errorf("setting ownership: %w", err)
	}

	// Clean up PostgreSQL configuration
	if err := s.updatePostgreSQLConfForTemplate(mountPath); err != nil {
		return nil, fmt.Errorf("updating PostgreSQL config: %w", err)
	}

	// Find available port
	port, err := findAvailablePortForInit()
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

	s.sendLog(stream, "INFO", "✓ System setup done")

	// Wait for PostgreSQL to be ready with journal log streaming
	s.sendLog(stream, "INFO", "Waiting for template to be ready...")

	if err := s.waitForPostgreSQLReadyWithJournalLogs(req.TemplateName, 30*time.Second, stream); err != nil {
		return nil, fmt.Errorf("waiting for PostgreSQL to be ready: %w", err)
	}

	// Store metadata (reuse existing logic)
	result := &InitResult{
		Dirname:     req.TemplateName,
		Stanza:      req.BackupToken.Stanza,
		Database:    req.Database,
		MountPath:   mountPath,
		Port:        port,
		ServiceName: serviceName,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	// Write metadata file to disk (same as InitRestore)
	if err := s.writeMetadataFile(result, mountPath); err != nil {
		return nil, fmt.Errorf("writing metadata file: %w", err)
	}

	s.sendLog(stream, "INFO", "✓ Template ready for branching")

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

func (s *AgentService) updatePostgreSQLConfForTemplate(mountPath string) error {
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

	// Comment out include_dir to avoid hugepages and other problematic settings
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

type InitResult struct {
	Dirname     string `json:"dirname"`
	Stanza      string `json:"stanza"`
	Database    string `json:"database"`
	MountPath   string `json:"mount_path"`
	Port        int    `json:"port"`
	ServiceName string `json:"service_name"`
	CreatedAt   string `json:"created_at"`
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

// multipass exec quic-checkout -- sudo journalctl -u quic-test-1757534974532537000-template --no-pager --since "17:09:00" | head -50
// ⎿ Sep 10 17:09:45 quic-checkout systemd[1]: Starting quic-test-1757534974532537000-template.service - PostgreSQL database server (restored instance -
//   test-1757534974532537000)...
//   Sep 10 17:09:45 quic-checkout postgres[7240]: [1-1] [7240][postmaster][][0] LOG:  redirecting log output to logging collector process
//   Sep 10 17:09:45 quic-checkout postgres[7240]: [1-2] [7240][postmaster][][0] HINT:  Future log output will appear in directory "log".
//   Sep 10 17:09:45 quic-checkout pg_ctl[7237]: waiting for server to start....
//   Sep 10 17:09:45 quic-checkout pg_ctl[7240]: [7240][postmaster][][0] LOG:  redirecting log output to logging collector process
//   Sep 10 17:09:45 quic-checkout pg_ctl[7240]: [7240][postmaster][][0] HINT:  Future log output will appear in directory "log".
//   Sep 10 17:09:45 quic-checkout postgres[7240]: [2-1] [7240][postmaster][][0] LOG:  ending log output to stderr
//   Sep 10 17:09:45 quic-checkout postgres[7240]: [2-2] [7240][postmaster][][0] HINT:  Future log output will go to log destination "syslog".
//   Sep 10 17:09:45 quic-checkout postgres[7240]: [3-1] [7240][postmaster][][0] LOG:  starting PostgreSQL 16.10 (Ubuntu 16.10-0ubuntu0.24.04.1) on
//   aarch64-unknown-linux-gnu, compiled by gcc (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0, 64-bit
//   Sep 10 17:09:45 quic-checkout postgres[7240]: [4-1] [7240][postmaster][][0] LOG:  listening on IPv4 address "127.0.0.1", port 15432
//   Sep 10 17:09:45 quic-checkout postgres[7240]: [5-1] [7240][postmaster][][0] LOG:  listening on Unix socket "/var/run/postgresql/.s.PGSQL.15432"
//   Sep 10 17:09:45 quic-checkout postgres[7244]: [6-1] [7244][startup][][0] LOG:  database system was interrupted; last known up at 2025-09-10 17:00:01 GMT
//   Sep 10 17:09:48 quic-checkout postgres[7244]: [7-1] [7244][startup][][0] LOG:  entering standby mode
//   Sep 10 17:09:48 quic-checkout postgres[7244]: [8-1] [7244][startup][][0] LOG:  starting backup recovery with redo LSN 0/30000028, checkpoint LSN 0/30000060, on
//   timeline ID 1
//   Sep 10 17:09:49 quic-checkout postgres[7244]: [9-1] [7244][startup][][0] LOG:  restored log file "000000010000000000000030" from archive
//   Sep 10 17:09:49 quic-checkout postgres[7244]: [10-1] [7244][startup][1/0][0] LOG:  redo starts at 0/30000028
//   Sep 10 17:09:50 quic-checkout postgres[7244]: [11-1] [7244][startup][1/0][0] LOG:  restored log file "000000010000000000000031" from archive
//   Sep 10 17:09:51 quic-checkout postgres[7244]: [12-1] [7244][startup][1/0][0] LOG:  restored log file "000000010000000000000032" from archive
//   Sep 10 17:09:52 quic-checkout postgres[7244]: [13-1] [7244][startup][1/0][0] LOG:  restored log file "000000010000000000000033" from archive
//   Sep 10 17:09:53 quic-checkout postgres[7244]: [14-1] [7244][startup][1/0][0] LOG:  completed backup recovery with redo LSN 0/30000028 and end LSN 0/31000050
//   Sep 10 17:09:53 quic-checkout postgres[7244]: [15-1] [7244][startup][1/0][0] LOG:  consistent recovery state reached at 0/31000050
//   Sep 10 17:09:53 quic-checkout postgres[7240]: [6-1] [7240][postmaster][][0] LOG:  database system is ready to accept read-only connections
//   Sep 10 17:09:53 quic-checkout pg_ctl[7237]: ...... done
//   Sep 10 17:09:53 quic-checkout pg_ctl[7237]: server started
//   Sep 10 17:09:53 quic-checkout systemd[1]: Started quic-test-1757534974532537000-template.service - PostgreSQL database server (restored instance -
//   test-1757534974532537000).

func (s *AgentService) waitForPostgreSQLReadyWithJournalLogs(templateName string, timeout time.Duration, stream pb.QuicService_RestoreTemplateServer) error {
	deadline := time.Now().Add(timeout)
	var lastCursor string // Empty string means start from recent logs
	serviceName := GetTemplateServiceName(templateName)

	for time.Now().Before(deadline) {
		// Check if PostgreSQL server is ready using pg_ctl status
		templatePath := fmt.Sprintf("/opt/quic/%s/_restore", templateName)
		if IsPostgreSQLServerReady(templatePath) {
			s.sendLog(stream, "INFO", "✓ PostgreSQL server is ready")
			return nil
		}

		// Stream recent journal logs for this service
		s.streamRecentJournalLogs(serviceName, &lastCursor, stream)

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("PostgreSQL not ready after %v timeout", timeout)
}

func (s *AgentService) streamRecentJournalLogs(serviceName string, lastCursor *string, stream pb.QuicService_RestoreTemplateServer) {
	args := []string{"sudo", "journalctl", "-u", serviceName, "--no-pager", "--output=json"}

	// Use cursor for precise positioning, fallback to service start time
	if *lastCursor != "" {
		args = append(args, "--after-cursor="+*lastCursor)
	} else {
		// Start from when the service was started/restarted
		args = append(args, "--since=@"+getServiceStartTime(serviceName))
	}

	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.Output()
	if err != nil {
		s.sendLog(stream, "WARN", fmt.Sprintf("Failed to read journal logs: %v", err))
		return
	}

	// Parse JSON output line by line
	for line := range strings.Lines(strings.TrimSpace(string(output))) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse JSON log entry
		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			continue // Skip malformed JSON
		}

		// Update cursor to this entry's cursor
		if cursor, ok := logEntry["__CURSOR"].(string); ok {
			*lastCursor = cursor
		}

		// Extract and send the message
		if message, ok := logEntry["MESSAGE"].(string); ok {
			// Format with timestamp for better readability
			timestamp := ""
			if ts, ok := logEntry["__REALTIME_TIMESTAMP"].(string); ok {
				if microseconds, err := strconv.ParseInt(ts, 10, 64); err == nil {
					timestamp = time.Unix(microseconds/1000000, (microseconds%1000000)*1000).Format("15:04:05")
				}
			}

			if timestamp != "" {
				s.sendLog(stream, "INFO", fmt.Sprintf("    [%s] %s", timestamp, message))
			} else {
				s.sendLog(stream, "INFO", fmt.Sprintf("    %s", message))
			}
		}
	}
}

// getServiceStartTime returns the Unix timestamp when the service was last started
func getServiceStartTime(serviceName string) string {
	cmd := exec.Command("sudo", "systemctl", "show", serviceName, "--property=ActiveEnterTimestamp", "--value")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to 10 seconds ago if we can't get service start time
		return strconv.FormatInt(time.Now().Add(-10*time.Second).Unix(), 10)
	}

	// Parse systemd timestamp format: "Tue 2025-09-10 17:09:53 UTC"
	timestampStr := strings.TrimSpace(string(output))
	if timestampStr == "" || timestampStr == "n/a" {
		// Service hasn't started yet, use current time
		return strconv.FormatInt(time.Now().Unix(), 10)
	}

	// Parse the timestamp
	parsedTime, err := time.Parse("Mon 2006-01-02 15:04:05 MST", timestampStr)
	if err != nil {
		// Fallback if parsing fails
		return strconv.FormatInt(time.Now().Add(-10*time.Second).Unix(), 10)
	}

	return strconv.FormatInt(parsedTime.Unix(), 10)
}
