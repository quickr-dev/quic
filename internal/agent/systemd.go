package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

func GetTemplateServiceName(template string) string {
	return fmt.Sprintf("quic-%s", template)
}

func GetBranchServiceName(template, branch string) string {
	return fmt.Sprintf("quic-%s-%s", template, branch)
}

func CreateTemplateService(templateName, mountPath string, port int) error {
	serviceName := GetTemplateServiceName(templateName)

	serviceContent := fmt.Sprintf(`[Unit]
Description=Quic template (%s)
After=network.target zfs-unlock.service

[Service]
Type=forking
User=postgres
ExecStart=%s start --pgdata=%s --options="--port=%d" --no-wait
ExecStop=%s stop --pgdata=%s --mode=fast
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutStartSec=10
TimeoutStopSec=30
Restart=on-failure
RestartSec=1

[Install]
WantedBy=multi-user.target
`, templateName, pgCtlPath(PgVersion), mountPath, port, pgCtlPath(PgVersion), mountPath)

	return writeSystemdService(serviceName, serviceContent)
}

func CreateBranchService(templateName, cloneName, clonePath string, port int) error {
	serviceName := fmt.Sprintf("quic-%s-%s", templateName, cloneName)

	serviceContent := fmt.Sprintf(`[Unit]
Description=Quic Branch (%s)
After=network.target

[Service]
Type=forking
User=postgres
ExecStart=%s start --pgdata=%s --options="--port=%d" --no-wait
ExecStop=%s stop --pgdata=%s --mode=fast
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutStartSec=10
TimeoutStopSec=30
Restart=on-failure
RestartSec=1

[Install]
WantedBy=multi-user.target
`, cloneName, pgCtlPath(PgVersion), clonePath, port, pgCtlPath(PgVersion), clonePath)

	return writeSystemdService(serviceName, serviceContent)
}

func StartService(serviceName string) error {
	err := exec.Command("sudo", "systemctl", "start", serviceName).Run()
	if err != nil {
		return fmt.Errorf("starting systemd service %s: %w", serviceName, err)
	}
	return nil
}

func StopService(serviceName string) error {
	if err := exec.Command("sudo", "systemctl", "stop", serviceName).Run(); err != nil {
		return fmt.Errorf("stopping systemd service %s: %w", serviceName, err)
	}
	return nil
}

func DeleteService(serviceName string) error {
	// Stop the service - ignore errors
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

func ServiceExists(serviceName string) bool {
	err := exec.Command("sudo", "systemctl", "cat", serviceName).Run()
	return err == nil
}

func writeSystemdService(serviceName, serviceContent string) error {
	serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)

	// Write service file
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
