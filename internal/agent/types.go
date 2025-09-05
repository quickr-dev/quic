package agent

import (
	"fmt"
	"strings"
	"time"
)

type CheckoutInfo struct {
	ID            int       `json:"id"`
	CloneName     string    `json:"clone_name"`
	Port          int       `json:"port"`
	ClonePath     string    `json:"clone_path"`
	AdminPassword string    `json:"admin_password"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (c *CheckoutInfo) ConnectionString(host string) string {
	return fmt.Sprintf("postgresql://admin:%s@%s:%d/postgres", c.AdminPassword, host, c.Port)
}

// GetRestoreNameFromCheckout derives the restore name from a checkout's clone path
// Expected clone path format: /opt/quic/RESTORE_NAME/CLONE_NAME
func (c *CheckoutInfo) GetRestoreName() string {
	if c.ClonePath == "" {
		return ""
	}

	parts := strings.Split(strings.TrimPrefix(c.ClonePath, "/"), "/")
	if len(parts) >= 3 && parts[0] == "opt" && parts[1] == "quic" {
		return parts[2] // Third part is the restore name (after "opt/quic")
	}

	return ""
}
