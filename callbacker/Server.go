package callbacker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ordishs/go-utils"
	"github.com/ordishs/gocore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/TAAL-GmbH/arc/callbacker/callbacker_api"
)

// Server type carries the logger within it
type Server struct {
	callbacker_api.UnsafeCallbackerAPIServer
	logger utils.Logger
}

// NewServer will return a server instance with the logger stored within it
func NewServer(logger utils.Logger) *Server {

	return &Server{
		logger: logger,
	}
}

// StartGRPCServer function
func (s *Server) StartGRPCServer() error {

	address, ok := gocore.Config().Get("callbacker_grpcAddress") //, "localhost:8002")
	if !ok {
		return errors.New("no callbacker_grpcAddress setting found")
	}

	// LEVEL 0 - no security / no encryption
	grpcServer := grpc.NewServer()

	gocore.SetAddress(address)

	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("GRPC server failed to listen [%w]", err)
	}

	callbacker_api.RegisterCallbackerAPIServer(grpcServer, s)

	// Register reflection service on gRPC server.
	reflection.Register(grpcServer)

	s.logger.Infof("GRPC server listening on %s", address)

	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("GRPC server failed [%w]", err)
	}

	return nil
}

func (s *Server) Health(_ context.Context, _ *emptypb.Empty) (*callbacker_api.HealthResponse, error) {
	return &callbacker_api.HealthResponse{
		Ok:        true,
		Timestamp: timestamppb.New(time.Now()),
	}, nil
}

func (s *Server) RegisterCallback(ctx context.Context, callback *callbacker_api.Callback) (*emptypb.Empty, error) {

	return &emptypb.Empty{}, nil
}
