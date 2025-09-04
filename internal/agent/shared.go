package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ZFS utilities
func (s *CheckoutService) datasetExists(dataset string) bool {
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", dataset)
	return cmd.Run() == nil
}

func (s *CheckoutService) snapshotExists(snapshot string) bool {
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", "-t", "snapshot", snapshot)
	return cmd.Run() == nil
}

// UFW/Firewall utilities
func (s *CheckoutService) openFirewallPort(port int) error {
	// Explicitly specify TCP protocol for enhanced security
	portSpec := fmt.Sprintf("%d/tcp", port)
	cmd := exec.Command("sudo", "ufw", "allow", portSpec)
	return cmd.Run()
}

func (s *CheckoutService) hasUFWRule(port int) bool {
	cmd := exec.Command("sudo", "ufw", "status")
	output, err := cmd.Output()
	if err != nil {
		return false // If we can't check UFW, assume no rule exists
	}

	portStr := fmt.Sprintf("%d/tcp", port)
	return strings.Contains(string(output), portStr)
}

func (s *CheckoutService) closeFirewallPort(port int) error {
	portSpec := fmt.Sprintf("%d/tcp", port)
	cmd := exec.Command("sudo", "ufw", "delete", "allow", portSpec)
	return cmd.Run()
}

// PostgreSQL utilities
func (s *CheckoutService) setPostgresPermissions(path string) error {
	// Permissions already set during cloud-init restore process
	// ZFS clones inherit permissions from parent dataset
	return nil
}

func (s *CheckoutService) ExecPostgresCommand(port int, database, sqlCommand string) (string, error) {
	psqlCmd := "/usr/lib/postgresql/16/bin/psql"
	socketDir := "/var/run/postgresql"

	cmd := exec.Command("sudo", "-u", "postgres", psqlCmd,
		"-h", socketDir,
		"-p", fmt.Sprintf("%d", port),
		"-d", database,
		"-c", sqlCommand)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql command failed: %w (output: %s)", err, string(output))
	}
	return string(output), nil
}

// Audit logging
func (s *CheckoutService) auditEvent(eventType string, details interface{}) error {
	logEntry := map[string]interface{}{
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"event_type": eventType,
		"details":    details,
	}

	logJSON, err := json.Marshal(logEntry)
	if err != nil {
		return fmt.Errorf("marshaling audit log entry: %w", err)
	}

	// Append to JSON log file following FHS standards
	logFile := "/var/log/quic/audit.log"
	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: failed to open audit log file: %v", err)
		return nil
	}
	defer file.Close()

	if _, err := file.WriteString(string(logJSON) + "\n"); err != nil {
		log.Printf("Warning: failed to write audit log: %v", err)
	}

	return nil
}

func ParseAuditEntry(line string) (map[string]interface{}, error) {
	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil, fmt.Errorf("unmarshaling audit entry: %w", err)
	}
	return entry, nil
}

// Helper functions for JSON parsing
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}
