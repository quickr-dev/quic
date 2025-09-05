package agent

import (
	"fmt"
	"os/exec"
)

func ExecPostgresCommand(port int, database, sqlCommand string) (string, error) {
	cmd := exec.Command("sudo", "-u", "postgres", psqlPath(PgVersion),
		"-h", PgSocketDir,
		"-p", fmt.Sprintf("%d", port),
		"-d", database,
		"-c", sqlCommand)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql command failed: %w (output: %s)", err, string(output))
	}
	return string(output), nil
}
