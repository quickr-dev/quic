package agent

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

type PostmasterPid struct {
	PID           string
	DataDirectory string
	StartTime     string
	Port          string
}

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
	postmasterPid, isRunning := getPostmasterPid(dataDir)
	if !isRunning {
		return false
	}

	// pg_isready output:
	// - not started: no response - exit status 2
	// - backup recovery mode: rejecting connections - exit status 1
	// - database system is ready to accept read-only connections: accepting connections - nil
	cmd := exec.Command("sudo", "-u", "postgres", pgIsReadyPath(PgVersion), "--port", postmasterPid.Port)
	output := cmd.Run()
	log.Println(">>>> pg_isready output: ", output)
	return output == nil
}

func getPostmasterPid(dataDir string) (PostmasterPid, bool) {
	content, err := exec.Command("sudo", "cat", dataDir+"/postmaster.pid").Output()
	if err != nil {
		return PostmasterPid{}, false
	}
	return parsePostmasterPid(string(content))
}

func parsePostmasterPid(content string) (PostmasterPid, bool) {
	lines := strings.Split(strings.TrimSpace(content), "\n")

	if len(lines) < 4 {
		return PostmasterPid{}, false
	}

	result := PostmasterPid{}
	result.PID = strings.TrimSpace(lines[0])
	result.DataDirectory = strings.TrimSpace(lines[1])
	result.StartTime = strings.TrimSpace(lines[2])
	result.Port = strings.TrimSpace(lines[3])

	return result, true
}
