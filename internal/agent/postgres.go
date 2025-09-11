package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

func ExecPostgresCommand(port int, database, sqlCommand string) (string, error) {
	cmd := exec.Command("sudo", "-u", "postgres", psqlPath(PgVersion),
		"-h", PgSocketDir,
		"-p", fmt.Sprintf("%d", port),
		"-d", database,
		"--no-align",
		"--tuples-only",
		"-c", sqlCommand)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql command failed: %w (output: %s)", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func IsPostgreSQLServerReady(dataDir string) bool {
	cmd := exec.Command("sudo", "-u", "postgres", pgCtlPath(PgVersion), "status", "-D", dataDir)
	// Exit code 0 means server is running and ready
	return cmd.Run() == nil
}
