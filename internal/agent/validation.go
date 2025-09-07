package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidateCloneName validates that a clone name is safe to use
// Rules: only letters, numbers, underscore, dash; no "_restore"; lowercase
func ValidateCloneName(name string) (string, error) {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Check length (reasonable limits)
	if len(name) < 1 || len(name) > 50 {
		return "", fmt.Errorf("clone name must be between 1 and 50 characters")
	}

	// Check for reserved name
	if name == "_restore" {
		return "", fmt.Errorf("clone name '_restore' is reserved")
	}

	// Check format: only alphanumeric, underscore, dash
	validName := regexp.MustCompile(`^[a-z0-9_-]+$`)
	if !validName.MatchString(name) {
		return "", fmt.Errorf("clone name must contain only letters, numbers, underscore, and dash")
	}

	return name, nil
}
