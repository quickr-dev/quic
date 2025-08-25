package server

import (
	"context"
	"fmt"
	"log"

	"github.com/quickr-dev/quic/internal/agent"
	"github.com/quickr-dev/quic/internal/auth"
	pb "github.com/quickr-dev/quic/proto"
)

// QuicServer implements the gRPC service interface
type QuicServer struct {
	pb.UnimplementedQuicServiceServer
	agentService *agent.CheckoutService
}

// NewQuicServer creates a new gRPC server instance
func NewQuicServer(agentService *agent.CheckoutService) *QuicServer {
	return &QuicServer{
		agentService: agentService,
	}
}

// CreateCheckout implements the CreateCheckout gRPC method
func (s *QuicServer) CreateCheckout(ctx context.Context, req *pb.CreateCheckoutRequest) (*pb.CreateCheckoutResponse, error) {
	user, ok := auth.GetUserFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("user not found in context")
	}

	log.Printf("User %s creating checkout: %s", user, req.CloneName)

	// Call the agent service to create the checkout
	checkout, err := s.agentService.CreateCheckout(ctx, req.CloneName)
	if err != nil {
		log.Printf("User %s failed to create checkout %s: %v", user, req.CloneName, err)
		return nil, fmt.Errorf("agent CreateCheckout failed: %w", err)
	}

	log.Printf("User %s successfully created checkout: %s", user, req.CloneName)

	// Return only the connection string as requested
	return &pb.CreateCheckoutResponse{
		ConnectionString: checkout.ConnectionString("localhost"),
	}, nil
}

// DeleteCheckout implements the DeleteCheckout gRPC method
func (s *QuicServer) DeleteCheckout(ctx context.Context, req *pb.DeleteCheckoutRequest) (*pb.DeleteCheckoutResponse, error) {
	user, ok := auth.GetUserFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("user not found in context")
	}

	log.Printf("User %s deleting checkout: %s", user, req.CloneName)

	// Call the agent service to delete the checkout
	deleted, err := s.agentService.DeleteCheckout(ctx, req.CloneName)
	if err != nil {
		log.Printf("User %s failed to delete checkout %s: %v", user, req.CloneName, err)
		return nil, fmt.Errorf("agent DeleteCheckout failed: %w", err)
	}

	log.Printf("User %s successfully deleted checkout %s (deleted: %v)", user, req.CloneName, deleted)

	return &pb.DeleteCheckoutResponse{
		Deleted: deleted,
	}, nil
}
