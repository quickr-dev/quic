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
	"github.com/spf13/cobra"
)

//go:embed assets/base-setup.yml
var baseSetupPlaybook string

//go:embed assets/ansible.cfg
var ansibleConfig string

var hostSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "[admin] Setup infrastructure on configured hosts",
	RunE:  runHostSetup,
}

func init() {
	hostSetupCmd.Flags().String("hosts", "", "Comma-separated list of host aliases, IPs, or 'all'")
}

func runHostSetup(cmd *cobra.Command, args []string) error {
	if err := checkAnsibleInstalled(); err != nil {
		return err
	}

	quicConfig, err := config.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("failed to load quic config: %w", err)
	}

	if len(quicConfig.Hosts) == 0 {
		return fmt.Errorf("no hosts configured in quic.json")
	}

	if err := validateQuicJSON(cmd, quicConfig); err != nil {
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
	}

	if !confirmDestructiveSetup() {
		fmt.Println("Setup aborted.")
		return nil
	}

	successCount := 0
	for _, host := range targetHosts {
		fmt.Printf("\nSetting up host %s (%s)...\n", host.IP, host.Alias)
		username := hostUsernames[host.IP]
		if err := setupHost(host, username); err != nil {
			fmt.Printf("Host %s setup failed: %v\n", host.IP, err)
			continue
		}
		if err := retrieveAndStoreCertificateFingerprint(quicConfig, host); err != nil {
			fmt.Printf("Warning: Failed to retrieve certificate fingerprint for %s: %v\n", host.IP, err)
			continue
		}
		successCount++
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

func confirmDestructiveSetup() bool {
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

	configFile, err := writeAnsibleConfigToTemp()
	if err != nil {
		return fmt.Errorf("failed to write ansible config: %w", err)
	}
	defer os.Remove(configFile)

	inventoryFile, err := createInventoryFile(host, username)
	if err != nil {
		return fmt.Errorf("failed to create inventory: %w", err)
	}
	defer os.Remove(inventoryFile)

	extraVars := fmt.Sprintf("zfs_devices=%s pg_version=16", strings.Join(host.Devices, ","))

	cmd := exec.Command("ansible-playbook",
		"-i", inventoryFile,
		"--extra-vars", extraVars,
		playbookFile)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "ANSIBLE_CONFIG="+configFile)

	return cmd.Run()
}

func writePlaybookToTemp() (string, error) {
	tmpFile := filepath.Join(os.TempDir(), "quic-base-setup-"+uuid.New().String()+".yml")
	return tmpFile, os.WriteFile(tmpFile, []byte(baseSetupPlaybook), 0644)
}

func writeAnsibleConfigToTemp() (string, error) {
	tmpFile := filepath.Join(os.TempDir(), "quic-ansible-"+uuid.New().String()+".cfg")
	return tmpFile, os.WriteFile(tmpFile, []byte(ansibleConfig), 0644)
}

func createInventoryFile(host config.QuicHost, username string) (string, error) {
	sshArgs := "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

	inventoryContent := fmt.Sprintf(`[quic_hosts]
%s ansible_user=%s ansible_become=yes ansible_ssh_common_args='%s'
`, host.IP, username, sshArgs)
	inventoryFile := filepath.Join(os.TempDir(), "quic-inventory-"+uuid.New().String())
	return inventoryFile, os.WriteFile(inventoryFile, []byte(inventoryContent), 0600)
}

func validateQuicJSON(cmd *cobra.Command, quicConfig *config.ProjectConfig) error {
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

func retrieveAndStoreCertificateFingerprint(projectConfig *config.ProjectConfig, host config.QuicHost) error {
	client, err := ssh.NewClient(host.IP)
	if err != nil {
		return fmt.Errorf("failed to connect via SSH: %w", err)
	}

	// Extract certificate fingerprint using OpenSSL
	fingerprintCmd := "openssl x509 -in /etc/quic/certs/server.crt -noout -fingerprint -sha256 | cut -d'=' -f2"
	output, err := client.RunCommand(fingerprintCmd)
	if err != nil {
		return fmt.Errorf("failed to extract certificate fingerprint: %w", err)
	}

	fingerprint := strings.TrimSpace(string(output))
	if fingerprint == "" {
		return fmt.Errorf("certificate fingerprint is empty")
	}

	// update the host certificate fingerprint
	if err := projectConfig.SetHostCertificateFingerprint(host.IP, fingerprint); err != nil {
		return fmt.Errorf("failed to save updated configuration: %w", err)
	}

	return nil
}
