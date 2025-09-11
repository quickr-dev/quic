package agent

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CreateTemplateService creates a systemd service for a PostgreSQL template
func CreateTemplateService(templateName, mountPath string, port int) error {
	serviceName := fmt.Sprintf("quic-%s-template", templateName)
	
	serviceContent := fmt.Sprintf(`[Unit]
Description=PostgreSQL database server (restored instance - %s)
Documentation=man:postgres(1)
After=network.target zfs-unlock.service

[Service]
Type=forking
User=postgres
ExecStart=%s start -D %s -o "--port=%d" -w -t 300
ExecStop=%s stop -D %s -m fast
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutStartSec=1200
TimeoutStopSec=300
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`, templateName, pgCtlPath(PgVersion), mountPath, port, pgCtlPath(PgVersion), mountPath)

	return writeSystemdService(serviceName, serviceContent)
}

// CreateCloneService creates a systemd service for a PostgreSQL clone
func CreateCloneService(templateName, cloneName, clonePath string, port int) error {
	serviceName := fmt.Sprintf("quic-%s-%s", templateName, cloneName)
	
	serviceContent := fmt.Sprintf(`[Unit]
Description=Quic PostgreSQL Clone (%s)
Documentation=https://github.com/quickr-dev/quic
After=network.target

[Service]
Type=forking
User=postgres
ExecStart=%s start -D %s -o "--port=%d" -w -t 300
ExecStop=%s stop -D %s -m fast
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutStartSec=1200
TimeoutStopSec=300
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`, cloneName, pgCtlPath(PgVersion), clonePath, port, pgCtlPath(PgVersion), clonePath)

	return writeSystemdService(serviceName, serviceContent)
}

// StartService starts a systemd service
func StartService(serviceName string) error {
	if err := exec.Command("sudo", "systemctl", "start", serviceName).Run(); err != nil {
		return fmt.Errorf("starting systemd service %s: %w", serviceName, err)
	}
	return nil
}

// StartCloneService starts a clone service with debugging and PostgreSQL readiness checks
func StartCloneService(templateName, cloneName string, port int) error {
	serviceName := GetCloneServiceName(templateName, cloneName)
	
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
	if err := waitForPostgresReady(port, 30*time.Second); err != nil {
		return fmt.Errorf("PostgreSQL failed to become ready on port %d: %w", port, err)
	}

	// Audit service start
	auditEvent("systemd_service_start", map[string]interface{}{
		"clone_name":   cloneName,
		"service_name": serviceName,
		"port":         port,
	})

	return nil
}

// StopService stops a systemd service
func StopService(serviceName string) error {
	if err := exec.Command("sudo", "systemctl", "stop", serviceName).Run(); err != nil {
		return fmt.Errorf("stopping systemd service %s: %w", serviceName, err)
	}
	return nil
}

// DeleteService removes a systemd service
func DeleteService(serviceName string) error {
	// Stop the service first (ignore errors if already stopped)
	exec.Command("sudo", "systemctl", "stop", serviceName).Run()
	
	// Disable the service
	if err := exec.Command("sudo", "systemctl", "disable", serviceName).Run(); err != nil {
		return fmt.Errorf("disabling systemd service %s: %w", serviceName, err)
	}
	
	// Remove service file
	serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	if err := exec.Command("sudo", "rm", "-f", serviceFilePath).Run(); err != nil {
		return fmt.Errorf("removing systemd service file %s: %w", serviceFilePath, err)
	}
	
	// Reload systemd daemon
	if err := exec.Command("sudo", "systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("reloading systemd daemon: %w", err)
	}
	
	return nil
}

// ServiceExists checks if a systemd service exists
func ServiceExists(serviceName string) bool {
	err := exec.Command("sudo", "systemctl", "cat", serviceName).Run()
	return err == nil
}

// GetServiceName returns the service name for a template
func GetTemplateServiceName(templateName string) string {
	return fmt.Sprintf("quic-%s-template", templateName)
}

// GetCloneServiceName returns the service name for a clone
func GetCloneServiceName(templateName, cloneName string) string {
	return fmt.Sprintf("quic-%s-%s", templateName, cloneName)
}

// writeSystemdService writes a systemd service file and enables it
func writeSystemdService(serviceName, serviceContent string) error {
	serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	
	// Write service file using sudo tee
	cmd := exec.Command("sudo", "tee", serviceFilePath)
	cmd.Stdin = strings.NewReader(serviceContent)
	cmd.Stdout = nil // Discard tee output
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