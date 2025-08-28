package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type CheckoutService struct {
	config *CheckoutConfig
	// Sync checkout flow
	checkoutMutex sync.Mutex
	// e.g. deployment restarting quicd
	shutdownSignal atomic.Bool
}

type CheckoutConfig struct {
	ZFSParentDataset string
	PostgresBinPath  string // /usr/lib/postgresql/16/bin
	StartPort        int
	EndPort          int
}

func NewCheckoutService(config *CheckoutConfig) *CheckoutService {
	return &CheckoutService{
		config: config,
	}
}

// Attempts to acquire the checkout lock while respecting shutdown signal.
// Returns true if lock acquired successfully, false if shutdown is in progress.
func (s *CheckoutService) tryLockWithShutdownCheck() bool {
	// Non-blocking check first to avoid unnecessary waiting
	if s.shutdownSignal.Load() {
		return false
	}

	s.checkoutMutex.Lock()

	// Double-check shutdown signal after acquiring lock
	// in case shutdown happened while waiting
	if s.shutdownSignal.Load() {
		s.checkoutMutex.Unlock()
		return false
	}

	return true
}

// Shutdown initiates graceful shutdown by rejecting new checkouts and waiting for active ones to complete.
// Only waits for the currently active checkout (if any), immediately rejects queued ones.
func (s *CheckoutService) Shutdown(timeout time.Duration) error {
	// Signal shutdown to reject new/queued requests
	s.shutdownSignal.Store(true)

	// Wait for active checkout to complete (if any)
	done := make(chan struct{})
	go func() {
		s.checkoutMutex.Lock()   // Wait for active operation to finish
		s.checkoutMutex.Unlock() // Release immediately
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("active checkout didn't complete within %v", timeout)
	}
}
