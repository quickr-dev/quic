package cli

import (
	"fmt"
	"strings"

	"github.com/quickr-dev/quic/internal/config"
	"github.com/quickr-dev/quic/internal/ssh"
	"github.com/quickr-dev/quic/internal/ui"
	"github.com/spf13/cobra"
)

var hostNewCmd = &cobra.Command{
	Use:   "new <ip>",
	Short: "Add a new host to quic configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runHostNew,
}

func init() {
	hostNewCmd.Flags().String("devices", "", "Comma-separated list of device names (e.g., loop10,loop11)")
}

func runHostNew(cmd *cobra.Command, args []string) error {
	ip := args[0]

	// Validate IP format
	if ip == "" {
		return fmt.Errorf("host IP cannot be empty")
	}

	client, err := ssh.NewClient(ip)
	if err != nil {
		return fmt.Errorf("failed to connect to host %s: %w\n\nTroubleshooting:\n• Ensure the host is reachable\n• Verify SSH is running on port 22\n• Check SSH agent is running: ssh-add -l\n• Verify root access: ssh root@%s", ip, err, ip)
	}
	defer client.Close()

	if err := client.TestConnection(); err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	if err := client.VerifyRootAccess(); err != nil {
		return fmt.Errorf("root access verification failed: %w\n\nTroubleshooting:\n• Ensure you can SSH as root: ssh root@%s\n• Or configure passwordless sudo for your user", err, ip)
	}

	devices, err := client.ListBlockDevices()
	if err != nil {
		return fmt.Errorf("failed to discover block devices: %w\n\nTroubleshooting:\n• Ensure lsblk command is available on the host\n• Verify the host has block devices available", err)
	}

	devicesFlag, _ := cmd.Flags().GetString("devices")
	var selectedDevices []string

	if devicesFlag != "" {
		// Use specified devices from flag
		specifiedDevices := strings.Split(devicesFlag, ",")
		for _, device := range specifiedDevices {
			device = strings.TrimSpace(device)
			// Validate that the device exists and is available
			found := false
			for _, d := range devices {
				if d.Name == device && d.Status == ssh.Available {
					selectedDevices = append(selectedDevices, device)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("device '%s' not found or not available", device)
			}
		}
	} else {
		// Interactive device selection
		availableDevices := client.GetAvailableDevices(devices)
		if len(availableDevices) == 0 {
			fmt.Println("\nNo available devices. Please, unmount or add storage devices.")
			fmt.Println("\nDiscovered devices:")
			printDeviceTable(devices)
			return nil
		}

		var err error
		selectedDevices, err = ui.RunDeviceSelector(devices)
		if err != nil {
			return fmt.Errorf("device selection failed: %w", err)
		}

		if len(selectedDevices) == 0 {
			fmt.Println("No devices selected. Exiting.")
			return nil
		}
	}

	quicConfig, err := config.LoadQuicConfig()
	if err != nil {
		return fmt.Errorf("failed to load quic config: %w", err)
	}

	host := config.QuicHost{
		IP:               ip,
		Alias:            "default",
		EncryptionAtRest: "localFile",
		Devices:          selectedDevices,
	}

	if err := quicConfig.AddHost(host); err != nil {
		return fmt.Errorf("failed to add host: %w", err)
	}

	if err := quicConfig.Save(); err != nil {
		return fmt.Errorf("failed to save quic config: %w", err)
	}

	fmt.Printf("✓ Added host '%s' (%s) to quic.json\n", host.Alias, ip)

	return nil
}

func printDeviceTable(devices []ssh.BlockDevice) {
	fmt.Printf("  %-20s %-10s %-10s %-15s\n", "NAME", "SIZE", "USED", "STATUS")
	for _, device := range devices {
		size := formatSize(device.Size)
		used := ""
		if device.FSSize != nil {
			used = formatSize(*device.FSSize)
		}

		status := string(device.Status)
		if device.Reason != "" {
			status += fmt.Sprintf(" (%s)", device.Reason)
		}

		fmt.Printf("  %-20s %-10s %-10s %-15s\n", device.Name, size, used, status)
	}
}

func formatSize(bytes int64) string {
	if bytes == 0 {
		return ""
	}

	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"K", "M", "G", "T", "P", "E"}
	return fmt.Sprintf("%.1f%s", float64(bytes)/float64(div), units[exp])
}