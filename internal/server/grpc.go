package server

import (
	"context"
	"fmt"
	"log"

	"github.com/quickr-dev/quic/internal/agent"
	"github.com/quickr-dev/quic/internal/auth"
	pb "github.com/quickr-dev/quic/proto"
)

type QuicServer struct {
	pb.UnimplementedQuicServiceServer
	agentService *agent.AgentService
}

func NewQuicServer(agentService *agent.AgentService) *QuicServer {
	return &QuicServer{
		agentService: agentService,
	}
}

func (s *QuicServer) CreateCheckout(ctx context.Context, req *pb.CreateCheckoutRequest) (*pb.CreateCheckoutResponse, error) {
	user, ok := auth.GetUserFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("user not found in context")
	}

	checkout, err := s.agentService.CreateBranch(ctx, req.CloneName, req.RestoreName, user)
	if err != nil {
		return nil, err
	}

	return &pb.CreateCheckoutResponse{
		ConnectionString: checkout.ConnectionString("localhost"),
	}, nil
}

func (s *QuicServer) DeleteCheckout(ctx context.Context, req *pb.DeleteCheckoutRequest) (*pb.DeleteCheckoutResponse, error) {
	// TODO: pass user to DeleteBranch
	// user, ok := auth.GetUserFromContext(ctx)
	// if !ok {
	// 	return nil, fmt.Errorf("user not found in context")
	// }

	deleted, err := s.agentService.DeleteBranch(ctx, req.RestoreName, req.CloneName)
	if err != nil {
		return nil, err
	}

	return &pb.DeleteCheckoutResponse{
		Deleted: deleted,
	}, nil
}

func (s *QuicServer) ListCheckouts(ctx context.Context, req *pb.ListCheckoutsRequest) (*pb.ListCheckoutsResponse, error) {
	checkouts, err := s.agentService.ListBranches(ctx, req.RestoreName)
	if err != nil {
		return nil, err
	}

	var pbCheckouts []*pb.CheckoutSummary
	for _, checkout := range checkouts {
		pbCheckout := &pb.CheckoutSummary{
			CloneName: checkout.BranchName,
			CreatedBy: checkout.CreatedBy,
			CreatedAt: checkout.CreatedAt.Format("2006-01-02 15:04:05"),
			Port:      checkout.Port,
		}
		pbCheckouts = append(pbCheckouts, pbCheckout)
	}

	return &pb.ListCheckoutsResponse{
		Checkouts: pbCheckouts,
	}, nil
}

func (s *QuicServer) RestoreTemplate(req *pb.RestoreTemplateRequest, stream pb.QuicService_RestoreTemplateServer) error {
	log.Printf("Restoring template: %s", req.TemplateName)

	return s.agentService.TemplateSetup(req, stream)
}
