package agent

import (
	"fmt"
	"strings"
	"time"
)

type CheckoutInfo struct {
	ID            int       `json:"id"`
	TemplateName  string    `json:"template_name"`
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

func (c *CheckoutInfo) GetRestoreName() string {
	if c.ClonePath == "" {
		return ""
	}

	// path format: /opt/quic/RESTORE_NAME/CLONE_NAME
	parts := strings.Split(strings.TrimPrefix(c.ClonePath, "/"), "/")
	if len(parts) >= 3 && parts[0] == "opt" && parts[1] == "quic" {
		return parts[2] // Third part is the restore name (after "opt/quic")
	}

	return ""
}
