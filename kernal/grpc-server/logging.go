package grpcserver

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aid297/rbac/kernal/logging"
	"github.com/aid297/rbac/kernal/rbac/persist"
)

var rpcLog logging.Logger

// SetLogger enables gRPC error logging (Internal, FailedPrecondition/pause, Unknown).
func SetLogger(l logging.Logger) {
	rpcLog = l
}

func unaryLoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		if rpcLog == nil || err == nil {
			return resp, err
		}
		st := status.Convert(err)
		code := st.Code()
		fields := []any{
			"method", info.FullMethod,
			"code", code.String(),
			"latency_ms", time.Since(start).Milliseconds(),
			"error", st.Message(),
		}
		switch code {
		case codes.FailedPrecondition:
			if paused, why := persist.Paused(); paused {
				fields = append(fields, "paused", true, "reason", why)
			}
			rpcLog.Warn("grpc request failed precondition", fields...)
		case codes.Internal, codes.Unknown:
			rpcLog.Error("grpc request failed", fields...)
		default:
			// InvalidArgument, NotFound, AlreadyExists: normal client errors — no log.
		}
		return resp, err
	}
}

func grpcServerOptions(extra ...grpc.ServerOption) []grpc.ServerOption {
	opts := []grpc.ServerOption{grpc.UnaryInterceptor(unaryLoggingInterceptor())}
	return append(opts, extra...)
}
