package ssh

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Client struct {
	client   *ssh.Client
	config   *ssh.ClientConfig
	username string
	useSudo  bool
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
	authMethods := []ssh.AuthMethod{}
	
	// Try SSH agent first
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		if conn, err := net.Dial("unix", socket); err == nil {
			agentClient := agent.NewClient(conn)
			authMethods = append(authMethods, ssh.PublicKeysCallback(agentClient.Signers))
		}
	}
	
	// Try to load default SSH keys if agent is not available
	if len(authMethods) == 0 {
		if homeDir, err := os.UserHomeDir(); err == nil {
			keyPaths := []string{
				filepath.Join(homeDir, ".ssh", "id_rsa"),
				filepath.Join(homeDir, ".ssh", "id_ed25519"),
				filepath.Join(homeDir, ".ssh", "id_ecdsa"),
			}
			
			for _, keyPath := range keyPaths {
				if key, err := os.ReadFile(keyPath); err == nil {
					if signer, err := ssh.ParsePrivateKey(key); err == nil {
						authMethods = append(authMethods, ssh.PublicKeys(signer))
						break
					}
				}
			}
		}
	}
	
	// If no auth methods available, return error
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH authentication methods available. Please ensure SSH agent is running or SSH keys are properly configured")
	}

	// Try connecting as different users (ubuntu first, then root)
	users := []string{"ubuntu", "root"}
	var lastErr error
	
	for _, user := range users {
		config := &ssh.ClientConfig{
			User:            user,
			Auth:            authMethods,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         10 * time.Second,
		}

		conn, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), config)
		if err != nil {
			lastErr = err
			continue
		}

		return &Client{
			client:   conn,
			config:   config,
			username: user,
			useSudo:  user != "root",
		}, nil
	}
	
	return nil, fmt.Errorf("failed to connect to %s as any user (tried: %s): %w", host, strings.Join(users, ", "), lastErr)
}

func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *Client) TestConnection() error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	if err := session.Run("echo 'connection test'"); err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	return nil
}

func (c *Client) runCommand(cmd string) ([]byte, error) {
	if c.useSudo {
		cmd = "sudo " + cmd
	}
	
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()
	
	return session.CombinedOutput(cmd)
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
	output, err := c.runCommand("lsblk --json -o NAME,SIZE,FSSIZE,MOUNTPOINTS -b")
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

