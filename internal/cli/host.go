package cli

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/quickr-dev/quic/internal/config"
	"github.com/quickr-dev/quic/internal/ssh"
	"github.com/quickr-dev/quic/internal/ui"
	"github.com/spf13/cobra"
)

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Manage quic hosts",
}

var hostNewCmd = &cobra.Command{
	Use:   "new <ip>",
	Short: "Add a new host to quic configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runHostNew,
}

var hostSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup infrastructure on configured hosts",
	RunE:  runHostSetup,
}

func init() {
	hostCmd.AddCommand(hostNewCmd)
	hostCmd.AddCommand(hostSetupCmd)
	hostNewCmd.Flags().String("devices", "", "Comma-separated list of device names (e.g., loop10,loop11)")
	hostSetupCmd.Flags().String("hosts", "", "Comma-separated list of host aliases, IPs, or 'all'")
}

//go:embed assets/base-setup.yml
var baseSetupPlaybook string

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

func runHostSetup(cmd *cobra.Command, args []string) error {
	if err := checkAnsibleInstalled(); err != nil {
		return err
	}

	quicConfig, err := config.LoadQuicConfig()
	if err != nil {
		return fmt.Errorf("failed to load quic config: %w", err)
	}

	if len(quicConfig.Hosts) == 0 {
		return fmt.Errorf("no hosts configured in quic.json")
	}

	if err := validateQuicJson(cmd, quicConfig); err != nil {
		return err
	}

	hostsFlag, _ := cmd.Flags().GetString("hosts")

	if len(quicConfig.Hosts) > 1 && hostsFlag == "" {
		cmd.PrintErrln("For safety, please specify the hosts to setup, for example:")
		cmd.PrintErrf("  $ quic host setup --hosts %s\n", quicConfig.Hosts[0].Alias)
		cmd.PrintErrf("  $ quic host setup --hosts %s\n", quicConfig.Hosts[0].IP)
		cmd.PrintErrln("  $ quic host setup --hosts all")
		return nil
	}

	targetHosts, err := filterHosts(cmd, quicConfig.Hosts, hostsFlag)
	if err != nil {
		return err
	}
	if targetHosts == nil {
		return nil
	}

	hostUsernames := make(map[string]string)
	for _, host := range targetHosts {
		client, err := ssh.NewClient(host.IP)
		if err != nil {
			return fmt.Errorf("failed to connect to host %s: %w", host.IP, err)
		}
		hostUsernames[host.IP] = client.Username()
		client.Close()
	}

	if !confirmDestructiveSetup(targetHosts) {
		fmt.Println("Setup aborted.")
		return nil
	}

	successCount := 0
	for _, host := range targetHosts {
		fmt.Printf("\nSetting up host %s (%s)...\n", host.IP, host.Alias)
		username := hostUsernames[host.IP]
		if err := setupHost(host, username); err != nil {
			fmt.Printf("✗ Host %s setup failed: %v\n", host.IP, err)
		} else {
			fmt.Printf("✓ Host %s setup completed successfully\n", host.IP)
			successCount++
		}
	}

	failedCount := len(targetHosts) - successCount
	fmt.Printf("\nSetup completed: %d successful, %d failed\n", successCount, failedCount)
	return nil
}

func checkAnsibleInstalled() error {
	_, err := exec.LookPath("ansible-playbook")
	if err != nil {
		return fmt.Errorf("ansible-playbook not found. Please install Ansible:\n" +
			"  macOS: brew install ansible\n" +
			"  Ubuntu: sudo apt install ansible\n" +
			"  pip: pip install ansible")
	}
	return nil
}

func confirmDestructiveSetup(hosts []config.QuicHost) bool {
	fmt.Println("WARNING: This will format devices and permanently delete all of their data.")
	fmt.Print("Type 'ack' to proceed: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return scanner.Text() == "ack"
}

func setupHost(host config.QuicHost, username string) error {
	playbookFile, err := writePlaybookToTemp()
	if err != nil {
		return fmt.Errorf("failed to write playbook: %w", err)
	}
	defer os.Remove(playbookFile)

	inventoryFile, err := createInventoryFile(host, username)
	if err != nil {
		return fmt.Errorf("failed to create inventory: %w", err)
	}
	defer os.Remove(inventoryFile)

	devicePaths := convertDevicesToPaths(host.Devices)
	extraVars := fmt.Sprintf("zfs_devices=%s pg_version=16", devicePaths)

	cmd := exec.Command("ansible-playbook",
		"-i", inventoryFile,
		"--extra-vars", extraVars,
		playbookFile)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func writePlaybookToTemp() (string, error) {
	tmpFile := filepath.Join(os.TempDir(), "quic-base-setup-"+uuid.New().String()+".yml")
	return tmpFile, os.WriteFile(tmpFile, []byte(baseSetupPlaybook), 0644)
}

func createInventoryFile(host config.QuicHost, username string) (string, error) {
	// Check if we're in test mode (test SSH key exists)
	testKeyPath := filepath.Join(os.TempDir(), "quic-test-ssh", "id_rsa")
	sshArgs := "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

	if _, err := os.Stat(testKeyPath); err == nil {
		sshArgs += " -i " + testKeyPath
	}

	inventoryContent := fmt.Sprintf(`[quic_hosts]
%s ansible_user=%s ansible_become=yes ansible_ssh_common_args='%s'
`, host.IP, username, sshArgs)
	inventoryFile := filepath.Join(os.TempDir(), "quic-inventory-"+uuid.New().String())
	return inventoryFile, os.WriteFile(inventoryFile, []byte(inventoryContent), 0600)
}

func convertDevicesToPaths(devices []string) string {
	paths := make([]string, len(devices))
	for i, device := range devices {
		paths[i] = "/dev/" + device
	}
	return strings.Join(paths, ",")
}

func validateQuicJson(cmd *cobra.Command, quicConfig *config.QuicConfig) error {
	aliases := make(map[string]bool)
	for _, host := range quicConfig.Hosts {
		if aliases[host.Alias] {
			cmd.PrintErrf("Duplicate host alias '%s' found in quic.json. Host aliases must be unique.\n", host.Alias)
			return nil
		}
		aliases[host.Alias] = true
	}
	return nil
}

func filterHosts(cmd *cobra.Command, allHosts []config.QuicHost, hostsFlag string) ([]config.QuicHost, error) {
	if hostsFlag == "" {
		return allHosts, nil
	}

	if hostsFlag == "all" {
		return allHosts, nil
	}

	hostSpecs := strings.Split(hostsFlag, ",")
	var targetHosts []config.QuicHost

	for _, spec := range hostSpecs {
		spec = strings.TrimSpace(spec)
		found := false

		for _, host := range allHosts {
			if host.Alias == spec || host.IP == spec {
				targetHosts = append(targetHosts, host)
				found = true
				break
			}
		}

		if !found {
			cmd.PrintErrf("Host '%s' not found in quic.json.\n", spec)
			cmd.PrintErrln("Available hosts:")
			for _, host := range allHosts {
				cmd.PrintErrf("  %s (%s)\n", host.Alias, host.IP)
			}
			return nil, nil
		}
	}

	return targetHosts, nil
}
