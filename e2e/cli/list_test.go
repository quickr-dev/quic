package e2e_cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuicList(t *testing.T) {
	checkoutOutput, templateName, branchName, err := setupQuicCheckout(t, QuicListVM)
	require.NoError(t, err, "checkout setup should succeed")
	require.Contains(t, checkoutOutput, "postgresql://admin", "checkout should return connection string")

	t.Run("ListAllBranches", func(t *testing.T) {
		// Test listing all branches
		listOutput, err := runQuic(t, "ls")
		require.NoError(t, err, "quic ls should succeed")

		// Should contain our branch
		require.Contains(t, listOutput, branchName, "list output should contain our branch")
		require.Contains(t, listOutput, "Test User", "list output should contain created by user")

		// Should have table headers
		require.Contains(t, listOutput, "BRANCH", "list output should have clone name header")
		require.Contains(t, listOutput, "CREATED BY", "list output should have created by header")
		require.Contains(t, listOutput, "CREATED AT", "list output should have created at header")
	})

	t.Run("ListBranchesFilteredByTemplate", func(t *testing.T) {
		// Test listing branches filtered by template
		listOutput, err := runQuic(t, "ls", "--template", templateName)
		require.NoError(t, err, "quic ls --template should succeed")

		// Should contain our branch
		require.Contains(t, listOutput, branchName, "filtered list should contain our branch")
		require.Contains(t, listOutput, "Test User", "filtered list should contain created by user")
	})

	t.Run("ListBranchesFromNonExistentTemplate", func(t *testing.T) {
		nonExistentTemplate := "non-existent-template"

		// Test listing branches from non-existent template
		listOutput, err := runQuic(t, "ls", "--template", nonExistentTemplate)
		require.NoError(t, err, "quic ls with non-existent template should succeed")

		// Should return "No checkouts found."
		require.Contains(t, listOutput, "No checkouts found.", "should show no checkouts message")
		require.NotContains(t, listOutput, branchName, "should not contain our branch")
	})

	t.Run("CreateMultipleBranchesAndList", func(t *testing.T) {
		// Create another branch in the same template
		secondBranchName := fmt.Sprintf("second-branch-%d", time.Now().UnixNano())
		secondCheckoutOutput, err := retryCheckoutUntilReady(t, secondBranchName, templateName, 30*time.Second)
		require.NoError(t, err, "second checkout should succeed")
		require.Contains(t, secondCheckoutOutput, "postgresql://admin", "second checkout should return connection string")

		// List branches for this template
		listOutput, err := runQuic(t, "ls", "--template", templateName)
		require.NoError(t, err, "quic ls --template should succeed")

		// Should contain both branches
		require.Contains(t, listOutput, branchName, "list should contain first branch")
		require.Contains(t, listOutput, secondBranchName, "list should contain second branch")

		// Count the number of branch lines (exclude header and separator lines)
		lines := strings.Split(listOutput, "\n")
		branchCount := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Skip empty lines, header lines, and separator lines
			if line == "" ||
				strings.Contains(line, "BRANCH") ||
				strings.Contains(line, "----------") {
				continue
			}
			branchCount++
		}
		require.Equal(t, 2, branchCount, "should list exactly 2 branches")
	})

	t.Run("ValidateBranchInfoInList", func(t *testing.T) {
		// List branches and validate the information displayed
		listOutput, err := runQuic(t, "ls", "--template", templateName)
		require.NoError(t, err, "quic ls --template should succeed")

		// Parse the output and find our branch
		lines := strings.Split(listOutput, "\n")
		var branchLine string
		for _, line := range lines {
			if strings.Contains(line, branchName) {
				branchLine = strings.TrimSpace(line)
				break
			}
		}
		require.NotEmpty(t, branchLine, "should find branch in list output")

		// Validate branch line contains expected information
		require.Contains(t, branchLine, branchName, "branch line should contain branch name")
		require.Contains(t, branchLine, "Test User", "branch line should contain created by")

		// Validate that the created at timestamp looks reasonable (not empty)
		parts := strings.Fields(branchLine)
		require.GreaterOrEqual(t, len(parts), 3, "branch line should have at least 3 parts")

		// The created at part should contain some timestamp-like content
		createdAtPart := strings.Join(parts[2:], " ") // Everything after the first two parts
		require.NotEmpty(t, createdAtPart, "created at field should not be empty")
	})

	t.Run("ValidateZFSDatasetStructure", func(t *testing.T) {
		// Verify the ZFS datasets follow the expected structure
		expectedDataset := fmt.Sprintf("tank/%s/%s", templateName, branchName)

		// Check that the dataset exists
		datasetOutput := runInVM(t, QuicListVM, "sudo zfs list", expectedDataset)
		require.Contains(t, datasetOutput, expectedDataset, "ZFS dataset should exist with correct structure")

		// Check that the mountpoint follows the expected pattern
		expectedMountpoint := fmt.Sprintf("/opt/quic/%s/%s", templateName, branchName)
		mountpointOutput := runInVM(t, QuicListVM, "sudo zfs get -H -o value mountpoint", expectedDataset)
		actualMountpoint := strings.TrimSpace(mountpointOutput)
		require.Equal(t, expectedMountpoint, actualMountpoint, "Mountpoint should follow the expected structure")

		// Verify metadata file exists in the correct location
		metadataPath := fmt.Sprintf("%s/.quic-meta.json", expectedMountpoint)
		runInVM(t, QuicListVM, "sudo test -f", metadataPath)
	})
}
