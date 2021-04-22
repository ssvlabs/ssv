package grpcex

import (
	"time"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/grpcutils"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
)

// Default values.
const (
	defaultMaxCallRecvMsgSize      = 10 * 5 << 20 // Default 50Mb
	defaultGRPCRetries        uint = 2
)

// DialConn dials GRPC connection
func DialConn(addr string) (*grpc.ClientConn, error) {
	baseOpts, err := constructDialOptions(defaultMaxCallRecvMsgSize, defaultGRPCRetries, )
	if err != nil {
		return nil, errors.Wrap(err, "failed to construct base dial options")
	}

	conn, err := grpc.Dial(addr, baseOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial context")
	}

	return conn, nil
}

// constructDialOptions constructs a list of grpc dial options
func constructDialOptions(
	maxCallRecvMsgSize int,
	grpcRetries uint,
	extraOpts ...grpc.DialOption,
) ([]grpc.DialOption, error) {
	if maxCallRecvMsgSize == 0 {
		maxCallRecvMsgSize = 10 * 5 << 20 // Default 50Mb
	}

	interceptors := []grpc.UnaryClientInterceptor{
		grpc_opentracing.UnaryClientInterceptor(),
		grpc_prometheus.UnaryClientInterceptor,
		grpc_retry.UnaryClientInterceptor(),
		grpcutils.LogGRPCRequests,
	}

	dialOpts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxCallRecvMsgSize),
			grpc_retry.WithMax(grpcRetries),
			grpc_retry.WithBackoff(grpc_retry.BackoffLinear(time.Second)),
		),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithUnaryInterceptor(middleware.ChainUnaryClient(interceptors...)),
		grpc.WithChainStreamInterceptor(
			grpcutils.LogGRPCStream,
			grpc_opentracing.StreamClientInterceptor(),
			grpc_prometheus.StreamClientInterceptor,
			grpc_retry.StreamClientInterceptor(),
		),
	}

	dialOpts = append(dialOpts, extraOpts...)
	return dialOpts, nil
}
