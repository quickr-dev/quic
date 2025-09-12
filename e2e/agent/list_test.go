package e2e_agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestList(t *testing.T) {
	// Setup: Create 2 restores with 2 clones each
	service1, restoreResult1 := createRestore(t)

	// First restore + clones
	clone1Name := generateCloneName()
	clone2Name := generateCloneName()
	_, err := service1.CreateBranch(context.Background(), clone1Name, restoreResult1.Dirname, createdBy)
	require.NoError(t, err)
	_, err = service1.CreateBranch(context.Background(), clone2Name, restoreResult1.Dirname, createdBy)
	require.NoError(t, err)

	// Second restore + clones
	service2, restoreResult2 := createRestore(t)
	clone3Name := generateCloneName()
	clone4Name := generateCloneName()
	_, err = service2.CreateBranch(context.Background(), clone3Name, restoreResult2.Dirname, createdBy)
	require.NoError(t, err)
	_, err = service2.CreateBranch(context.Background(), clone4Name, restoreResult2.Dirname, createdBy)
	require.NoError(t, err)

	t.Run("ListAllCheckouts", func(t *testing.T) {
		// List all checkouts (no filter)
		checkouts, err := service1.ListBranches(context.Background(), "")
		require.NoError(t, err, "ListCheckouts should succeed")

		// Verify all our clones are in the list
		foundClones := make(map[string]bool)
		for _, checkout := range checkouts {
			foundClones[checkout.BranchName] = true
		}
		require.True(t, foundClones[clone1Name], "Should find clone1")
		require.True(t, foundClones[clone2Name], "Should find clone2")
		require.True(t, foundClones[clone3Name], "Should find clone3")
		require.True(t, foundClones[clone4Name], "Should find clone4")
	})

	t.Run("ListCheckoutsFilteredByFirstRestore", func(t *testing.T) {
		// List checkouts filtered by first restore
		checkouts, err := service1.ListBranches(context.Background(), restoreResult1.Dirname)
		require.NoError(t, err, "ListCheckouts should succeed")
		require.Equal(t, 2, len(checkouts), "Should find exactly 2 checkouts in first restore")

		// Verify only clones from first restore are returned
		foundClones := make(map[string]bool)
		for _, checkout := range checkouts {
			foundClones[checkout.BranchName] = true
			// All returned checkouts should be from the first restore
			require.Equal(t, restoreResult1.Dirname, checkout.GetRestoreName(), "All checkouts should belong to first restore")
		}
		require.True(t, foundClones[clone1Name], "Should find clone1 from first restore")
		require.True(t, foundClones[clone2Name], "Should find clone2 from first restore")
		require.False(t, foundClones[clone3Name], "Should not find clone3 from second restore")
		require.False(t, foundClones[clone4Name], "Should not find clone4 from second restore")
	})

	t.Run("ListCheckoutsFilteredBySecondRestore", func(t *testing.T) {
		// List checkouts filtered by second restore
		checkouts, err := service2.ListBranches(context.Background(), restoreResult2.Dirname)
		require.NoError(t, err, "ListCheckouts should succeed")
		require.Equal(t, 2, len(checkouts), "Should find exactly 2 checkouts in second restore")

		// Verify only clones from second restore are returned
		foundClones := make(map[string]bool)
		for _, checkout := range checkouts {
			foundClones[checkout.BranchName] = true
			// All returned checkouts should be from the second restore
			require.Equal(t, restoreResult2.Dirname, checkout.GetRestoreName(), "All checkouts should belong to second restore")
		}
		require.True(t, foundClones[clone3Name], "Should find clone3 from second restore")
		require.True(t, foundClones[clone4Name], "Should find clone4 from second restore")
		require.False(t, foundClones[clone1Name], "Should not find clone1 from first restore")
		require.False(t, foundClones[clone2Name], "Should not find clone2 from first restore")
	})

	t.Run("ListCheckoutsFromNonExistentRestore", func(t *testing.T) {
		nonExistentRestoreName := "non-existent-restore"

		// List checkouts from a non-existent restore
		checkouts, err := service1.ListBranches(context.Background(), nonExistentRestoreName)
		require.NoError(t, err, "ListCheckouts should not error for non-existent restore")
		require.Equal(t, 0, len(checkouts), "Should return empty list for non-existent restore")
	})

	t.Run("ListEmptyRestore", func(t *testing.T) {
		// Create a new restore with no checkouts
		emptyService, emptyRestoreResult := createRestore(t)

		// List checkouts from the empty restore
		checkouts, err := emptyService.ListBranches(context.Background(), emptyRestoreResult.Dirname)
		require.NoError(t, err, "ListCheckouts should succeed for empty restore")
		require.Equal(t, 0, len(checkouts), "Should return empty list for restore with no checkouts")
	})

	t.Run("VerifyCheckoutInfo", func(t *testing.T) {
		// Use one of the pre-created checkouts to verify info
		checkouts, err := service1.ListBranches(context.Background(), restoreResult1.Dirname)
		require.NoError(t, err, "ListCheckouts should succeed")
		require.GreaterOrEqual(t, len(checkouts), 1, "Should find at least 1 checkout")

		// Get the first checkout for verification
		foundCheckout := checkouts[0]

		// Verify checkout info fields
		require.NotEmpty(t, foundCheckout.BranchName, "Clone name should not be empty")
		require.Equal(t, restoreResult1.Dirname, foundCheckout.GetRestoreName(), "Restore name should match")
		require.Equal(t, createdBy, foundCheckout.CreatedBy, "Created by should match")
		require.Greater(t, foundCheckout.Port, 0, "Port should be positive")
		require.NotZero(t, foundCheckout.CreatedAt, "CreatedAt should be set")
		require.NotEmpty(t, foundCheckout.ConnectionString("localhost"), "Connection string should not be empty")
	})

}
