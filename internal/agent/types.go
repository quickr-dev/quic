package agent

import (
	"fmt"
	"strings"
	"time"
)

type BranchInfo struct {
	ID            int       `json:"id"`
	TemplateName  string    `json:"template_name"`
	BranchName    string    `json:"clone_name"`
	Port          string    `json:"port"`
	BranchPath    string    `json:"clone_path"`
	AdminPassword string    `json:"admin_password"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (c *BranchInfo) ConnectionString(host string) string {
	return fmt.Sprintf("postgresql://admin:%s@%s:%s/postgres", c.AdminPassword, host, c.Port)
}

func (c *BranchInfo) GetRestoreName() string {
	if c.BranchPath == "" {
		return ""
	}

	// path format: /opt/quic/RESTORE_NAME/CLONE_NAME
	parts := strings.Split(strings.TrimPrefix(c.BranchPath, "/"), "/")
	if len(parts) >= 3 && parts[0] == "opt" && parts[1] == "quic" {
		return parts[2] // Third part is the restore name (after "opt/quic")
	}

	return ""
}
