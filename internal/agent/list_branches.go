package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func (s *AgentService) ListBranches(ctx context.Context, filterByTemplateName string) ([]*CheckoutInfo, error) {
	var searchDataset string
	if filterByTemplateName != "" {
		searchDataset = ZPool + "/" + filterByTemplateName
	} else {
		searchDataset = ZPool
	}

	// List all datasets recursively under the search dataset
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", "-r", searchDataset)
	output, err := cmd.Output()
	if err != nil {
		// If the specific restore doesn't exist, return empty list instead of error
		if filterByTemplateName != "" {
			return []*CheckoutInfo{}, nil
		}
		return nil, fmt.Errorf("listing ZFS datasets: %w", err)
	}

	var checkouts []*CheckoutInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == searchDataset {
			continue // Skip empty lines and search dataset itself
		}

		// Skip the _restore datasets
		if strings.HasSuffix(line, "/_restore") {
			continue
		}

		// Extract clone name and restore name from dataset path
		// Expected format: tank/RESTORE_NAME/CLONE_NAME
		parts := strings.Split(line, "/")
		if len(parts) < 3 {
			continue // Invalid format - need at least tank/restore/clone
		}

		templateName := parts[len(parts)-2]
		cloneName := parts[len(parts)-1]

		// Try to get checkout info for this clone
		checkout, err := s.discoverCheckoutFromOS(templateName, cloneName)
		if err != nil {
			// Log the error but continue with other checkouts
			fmt.Printf("Warning: failed to load checkout info for %s/%s: %v\n", templateName, cloneName, err)
			continue
		}

		if checkout != nil {
			checkouts = append(checkouts, checkout)
		}
	}

	return checkouts, nil
}
