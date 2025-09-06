package ssh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Client struct {
	host     string
	username string
	useSudo  bool
	sshArgs  []string
}

type BlockDevice struct {
	Name         string        `json:"name"`
	Size         int64         `json:"size"`
	FSSize       *int64        `json:"fssize"`
	Mountpoints  []string      `json:"mountpoints"`
	Children     []BlockDevice `json:"children"`
	Status       DeviceStatus
	Reason       string
}

type DeviceStatus string

const (
	Available DeviceStatus = "available"
	Mounted   DeviceStatus = "mounted"
	SystemDisk DeviceStatus = "system"
)

type lsblkOutput struct {
	Blockdevices []BlockDevice `json:"blockdevices"`
}

func NewClient(host string) (*Client, error) {
	// Try connecting as different users (ubuntu first, then root)
	users := []string{"ubuntu", "root"}
	
	baseSSHArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		"-o", "BatchMode=yes", // Don't prompt for passwords
		"-o", "LogLevel=ERROR", // Suppress SSH warnings
	}
	
	// Check if we have a test SSH key (for e2e tests)
	testKeyPath := filepath.Join(os.TempDir(), "quic-test-ssh", "id_rsa")
	if _, err := os.Stat(testKeyPath); err == nil {
		baseSSHArgs = append(baseSSHArgs, "-i", testKeyPath)
	}
	
	for _, user := range users {
		sshArgs := append(baseSSHArgs, "-l", user)
		
		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "ssh", append(sshArgs, host, "echo", "test")...)
		err := cmd.Run()
		cancel()
		
		if err == nil {
			return &Client{
				host:     host,
				username: user,
				useSudo:  user != "root",
				sshArgs:  sshArgs,
			}, nil
		}
	}
	
	return nil, fmt.Errorf("failed to connect to %s as any user (tried: %s): SSH authentication failed. Please ensure SSH keys are configured or SSH agent is running", host, strings.Join(users, ", "))
}

func (c *Client) Close() error {
	// No persistent connection to close when using system ssh
	return nil
}

func (c *Client) Username() string {
	return c.username
}

func (c *Client) TestConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, "ssh", append(c.sshArgs, c.host, "echo", "connection test")...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	return nil
}

func (c *Client) runCommand(cmd string) ([]byte, error) {
	return c.runCommandWithStderr(cmd, false)
}

func (c *Client) runCommandWithStderr(cmd string, includeStderr bool) ([]byte, error) {
	if c.useSudo {
		cmd = "sudo " + cmd
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	sshCmd := exec.CommandContext(ctx, "ssh", append(c.sshArgs, c.host, cmd)...)
	
	if includeStderr {
		return sshCmd.CombinedOutput()
	} else {
		// Use Output() to only capture stdout, ignore stderr SSH warnings
		return sshCmd.Output()
	}
}

func (c *Client) VerifyRootAccess() error {
	// Test basic connectivity
	output, err := c.runCommand("whoami")
	if err != nil {
		return fmt.Errorf("failed to check user: %w", err)
	}
	
	currentUser := strings.TrimSpace(string(output))
	
	// If we're already root, we're good
	if currentUser == "root" {
		return nil
	}
	
	// If we're using sudo, test that we can become root
	if c.useSudo {
		rootOutput, err := c.runCommand("whoami")
		if err != nil {
			return fmt.Errorf("failed to verify sudo access: %w", err)
		}
		
		sudoUser := strings.TrimSpace(string(rootOutput))
		if sudoUser != "root" {
			return fmt.Errorf("sudo access verification failed. Expected root, got: %s", sudoUser)
		}
		
		return nil
	}
	
	return fmt.Errorf("root access required, current user: %s", currentUser)
}

func (c *Client) ListBlockDevices() ([]BlockDevice, error) {
	output, err := c.runCommandWithStderr("lsblk --json -o NAME,SIZE,FSSIZE,MOUNTPOINTS -b", true)
	if err != nil {
		return nil, fmt.Errorf("failed to list block devices: %w", err)
	}

	var lsblk lsblkOutput
	if err := json.Unmarshal(output, &lsblk); err != nil {
		return nil, fmt.Errorf("failed to parse lsblk output: %w", err)
	}

	var devices []BlockDevice
	for _, device := range lsblk.Blockdevices {
		device = c.analyzeDevice(device)
		devices = append(devices, device)
		
		// Add child devices (partitions) if they exist
		devices = append(devices, c.processChildDevices(device)...)
	}

	// Sort devices by size descending (largest first)
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Size > devices[j].Size
	})

	return devices, nil
}

func (c *Client) analyzeDevice(device BlockDevice) BlockDevice {
	// Check if device is mounted
	for _, mountpoint := range device.Mountpoints {
		if mountpoint != "" {
			device.Status = Mounted
			device.Reason = fmt.Sprintf("mounted at %s", mountpoint)
			return device
		}
	}

	// Check if any child devices are mounted
	for _, child := range device.Children {
		for _, mountpoint := range child.Mountpoints {
			if mountpoint != "" {
				device.Status = Mounted
				device.Reason = "has mounted partitions"
				return device
			}
		}
	}

	// Check for system devices (common patterns)
	if c.isSystemDevice(device.Name) {
		device.Status = SystemDisk
		device.Reason = "system device"
		return device
	}

	device.Status = Available
	return device
}

func (c *Client) processChildDevices(parent BlockDevice) []BlockDevice {
	// This would need to be implemented if we want to show partitions
	// For now, we'll keep it simple and only show top-level devices
	return nil
}

func (c *Client) isSystemDevice(name string) bool {
	systemPatterns := []string{
		"sr",    // CD/DVD drives
		"dm-",   // Device mapper
	}

	for _, pattern := range systemPatterns {
		if strings.Contains(name, pattern) {
			return true
		}
	}

	return false
}

func (c *Client) GetAvailableDevices(devices []BlockDevice) []BlockDevice {
	var available []BlockDevice
	for _, device := range devices {
		if device.Status == Available {
			available = append(available, device)
		}
	}
	return available
}

