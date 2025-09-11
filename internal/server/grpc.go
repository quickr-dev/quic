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
	agentService *agent.AgentService
}

// NewQuicServer creates a new gRPC server instance
func NewQuicServer(agentService *agent.AgentService) *QuicServer {
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
	checkout, err := s.agentService.CreateBranch(ctx, req.CloneName, req.RestoreName, user)
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
	deleted, err := s.agentService.DeleteBranch(ctx, req.CloneName, req.RestoreName)
	if err != nil {
		log.Printf("User %s failed to delete checkout %s: %v", user, req.CloneName, err)
		return nil, fmt.Errorf("agent DeleteCheckout failed: %w", err)
	}

	log.Printf("User %s successfully deleted checkout %s (deleted: %v)", user, req.CloneName, deleted)

	return &pb.DeleteCheckoutResponse{
		Deleted: deleted,
	}, nil
}

// ListCheckouts implements the ListCheckouts gRPC method
func (s *QuicServer) ListCheckouts(ctx context.Context, req *pb.ListCheckoutsRequest) (*pb.ListCheckoutsResponse, error) {
	user, ok := auth.GetUserFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("user not found in context")
	}

	log.Printf("User %s listing checkouts", user)

	// Call the agent service to list checkouts
	checkouts, err := s.agentService.ListBranches(ctx, req.RestoreName)
	if err != nil {
		log.Printf("User %s failed to list checkouts: %v", user, err)
		return nil, fmt.Errorf("agent ListCheckouts failed: %w", err)
	}

	// Convert agent.CheckoutInfo to pb.CheckoutSummary
	var pbCheckouts []*pb.CheckoutSummary
	for _, checkout := range checkouts {
		pbCheckout := &pb.CheckoutSummary{
			CloneName: checkout.CloneName,
			CreatedBy: checkout.CreatedBy,
			CreatedAt: checkout.CreatedAt.Format("2006-01-02 15:04:05"), // User-friendly format
			Port:      int32(checkout.Port),
		}
		pbCheckouts = append(pbCheckouts, pbCheckout)
	}

	log.Printf("User %s successfully retrieved %d checkouts", user, len(pbCheckouts))

	return &pb.ListCheckoutsResponse{
		Checkouts: pbCheckouts,
	}, nil
}

// RestoreTemplate implements the RestoreTemplate gRPC method
func (s *QuicServer) RestoreTemplate(req *pb.RestoreTemplateRequest, stream pb.QuicService_RestoreTemplateServer) error {
	log.Printf("Restoring template: %s", req.TemplateName)

	// Call the agent service to restore the template
	return s.agentService.TemplateSetup(req, stream)
}
