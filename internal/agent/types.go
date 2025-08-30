package agent

import (
	"fmt"
	"regexp"
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
	return fmt.Sprintf("postgresql://admin:%s@%s:%d/postgres",
		c.AdminPassword, host, c.Port)
}

type ZFSConfig struct {
	ParentDataset string
}

func (z *ZFSConfig) RestoreDataset() string {
	return z.ParentDataset + "/_restore"
}

func (z *ZFSConfig) CloneDataset(cloneName string) string {
	return z.ParentDataset + "/" + cloneName
}

// ValidateCloneName validates that a clone name is safe to use
// Rules: only letters, numbers, underscore, dash; no "_restore"; lowercase
func ValidateCloneName(name string) (string, error) {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Check for reserved name
	if name == "_restore" {
		return "", fmt.Errorf("clone name '_restore' is reserved")
	}

	// Check format: only alphanumeric, underscore, dash
	validName := regexp.MustCompile(`^[a-z0-9_-]+$`)
	if !validName.MatchString(name) {
		return "", fmt.Errorf("clone name must contain only letters, numbers, underscore, and dash")
	}

	// Check length (reasonable limits)
	if len(name) < 1 || len(name) > 63 {
		return "", fmt.Errorf("clone name must be between 1 and 63 characters")
	}

	return name, nil
}
