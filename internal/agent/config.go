package agent

import "fmt"

const (
	ZPool       = "tank"
	PgVersion   = "16"
	StartPort   = 15432
	EndPort     = 16432
	LogFile     = "/var/log/quic/audit.log"
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
