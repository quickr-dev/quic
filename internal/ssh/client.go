package ssh

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	host     string
	username string
	useSudo  bool
	sshArgs  []string
}

// FlexibleInt64 handles JSON fields that can be either int64 or string
type FlexibleInt64 struct {
	Value *int64
}

func (f *FlexibleInt64) UnmarshalJSON(data []byte) error {
	// Handle null values
	if string(data) == "null" {
		f.Value = nil
		return nil
	}

	// Try to unmarshal as int64 first
	var intVal int64
	if err := json.Unmarshal(data, &intVal); err == nil {
		f.Value = &intVal
		return nil
	}

	// Try to unmarshal as string and convert
	var strVal string
	if err := json.Unmarshal(data, &strVal); err != nil {
		return err
	}

	intVal, err := strconv.ParseInt(strVal, 10, 64)
	if err != nil {
		return err
	}
	f.Value = &intVal
	return nil
}

type BlockDevice struct {
	Name        string        `json:"name"`
	Size        FlexibleInt64 `json:"size"`
	FSSize      FlexibleInt64 `json:"fssize"`
	Mountpoints []string      `json:"mountpoints"`
	Children    []BlockDevice `json:"children"`
	Status      DeviceStatus
	Reason      string
}

type DeviceStatus string

const (
	Available  DeviceStatus = "available"
	Mounted    DeviceStatus = "mounted"
	SystemDisk DeviceStatus = "system"
)

type lsblkOutput struct {
	Blockdevices []BlockDevice `json:"blockdevices"`
}

func NewClient(host string) (*Client, error) {
	// Try connecting as different users
	users := []string{"ec2-user", "ubuntu", "root"}

	baseSSHArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		"-o", "BatchMode=yes", // Don't prompt for passwords
		"-o", "LogLevel=ERROR", // Suppress SSH warnings
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

	return nil, fmt.Errorf("failed to ssh to %s. Tried users: %s", host, strings.Join(users, ", "))
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

func (c *Client) RunCommand(cmd string) ([]byte, error) {
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
	output, err := c.RunCommand("whoami")
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
		rootOutput, err := c.RunCommand("whoami")
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
	}

	// Sort devices by size descending (largest first)
	sort.Slice(devices, func(i, j int) bool {
		sizeI := int64(0)
		if devices[i].Size.Value != nil {
			sizeI = *devices[i].Size.Value
		}
		sizeJ := int64(0)
		if devices[j].Size.Value != nil {
			sizeJ = *devices[j].Size.Value
		}
		return sizeI > sizeJ
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

func (c *Client) isSystemDevice(name string) bool {
	systemPatterns := []string{
		"sr",  // CD/DVD drives
		"dm-", // Device mapper
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

func (c *Client) TestPath(path string) error {
	_, err := c.RunCommand(fmt.Sprintf("test -e %s", path))
	if err != nil {
		return fmt.Errorf("path does not exist or is not accessible")
	}
	return nil
}
