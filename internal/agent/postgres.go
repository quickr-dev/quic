package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	PgVersion   = "16"
	StartPort   = 15432
	EndPort     = 16432
	PgSocketDir = "/var/run/postgresql"
)

func psqlPath(pgVersion string) string {
	return fmt.Sprintf("/usr/lib/postgresql/%s/bin/psql", pgVersion)
}

func pgCtlPath(pgVersion string) string {
	return fmt.Sprintf("/usr/lib/postgresql/%s/bin/pg_ctl", pgVersion)
}

func pgResetWalPath(pgVersion string) string {
	return fmt.Sprintf("/usr/lib/postgresql/%s/bin/pg_resetwal", pgVersion)
}

func pgIsReadyPath(pgVersion string) string {
	return fmt.Sprintf("/usr/lib/postgresql/%s/bin/pg_isready", pgVersion)
}

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
	cmd := exec.Command("sudo", "-u", "postgres", pgIsReadyPath(PgVersion), "-h", PgSocketDir)
	return cmd.Run() == nil
}
