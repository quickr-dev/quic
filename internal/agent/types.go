package agent

import (
	"fmt"
	"time"
)

type BranchInfo struct {
	ID            int       `json:"id"`
	TemplateName  string    `json:"template_name"`
	BranchName    string    `json:"branch_name"`
	Port          string    `json:"port"`
	BranchPath    string    `json:"branch_path"`
	AdminPassword string    `json:"admin_password"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (c *BranchInfo) ConnectionString(host string) string {
	return fmt.Sprintf("postgresql://admin:%s@%s:%s/postgres", c.AdminPassword, host, c.Port)
}
