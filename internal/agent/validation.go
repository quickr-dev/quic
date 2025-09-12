package agent

import (
	"fmt"
	"regexp"
	"strings"
)

func ValidateBranchName(name string) (string, error) {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Check length
	if len(name) < 1 || len(name) > 50 {
		return "", fmt.Errorf("branch name must be between 1 and 50 characters")
	}

	// Check for reserved name
	if name == "_restore" {
		return "", fmt.Errorf("branch name '_restore' is reserved")
	}

	// Check format: only alphanumeric, underscore, dash
	validName := regexp.MustCompile(`^[a-z0-9_-]+$`)
	if !validName.MatchString(name) {
		return "", fmt.Errorf("branch name must contain only letters, numbers, underscore, and dash")
	}

	return name, nil
}
