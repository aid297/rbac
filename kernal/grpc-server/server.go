package grpcserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	rbacv1 "github.com/aid297/rbac/kernal/api/gen/rbac/v1"
	"github.com/aid297/rbac/kernal/rbac/persist"
	"github.com/aid297/rbac/kernal/svcctl"
)

// Options configures gRPC and/or gRPC+TLS listeners.
type Options struct {
	// Addr is the plaintext gRPC listen address (e.g. "0.0.0.0:9080"). Empty skips it.
	Addr string
	// TLSAddr is the TLS gRPC listen address. Empty skips it.
	TLSAddr string
	// TLSCert is required when TLSAddr is set.
	TLSCert *tls.Certificate
	// CACertPEM is served by GetCACert.
	CACertPEM []byte
	// Gate tracks in-flight RPCs for persist.ServiceControl.Pause.
	// If nil and OwnServiceControl is true, a private gate is created and registered.
	Gate *svcctl.Gate
	// OwnServiceControl registers Gate (or a private one) with persist when true.
	// Prefer sharing one Gate from main when HTTP and gRPC both run.
	OwnServiceControl bool
}

// Serve runs plaintext and/or TLS gRPC until ctx is cancelled. Empty addrs are skipped.
func Serve(ctx context.Context, store *persist.Store, opt Options) error {
	if store == nil {
		return fmt.Errorf("grpcserver: nil store")
	}
	if opt.Addr == "" && opt.TLSAddr == "" {
		return nil
	}
	if opt.TLSAddr != "" && opt.TLSCert == nil {
		return fmt.Errorf("grpcserver: TLS certificate required for gRPC TLS")
	}

	gate := opt.Gate
	if gate == nil {
		gate = new(svcctl.Gate)
		if opt.OwnServiceControl {
			persist.SetServiceControl(gate)
			defer persist.SetServiceControl(nil)
		}
	} else if opt.OwnServiceControl {
		persist.SetServiceControl(gate)
		defer persist.SetServiceControl(nil)
	}

	svc := NewServer(store, opt.CACertPEM, gate)
	var (
		servers []*grpc.Server
		errCh   = make(chan error, 2)
		wg      sync.WaitGroup
	)

	start := func(server *grpc.Server, lis net.Listener) {
		servers = append(servers, server)
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- server.Serve(lis)
		}()
	}

	if opt.Addr != "" {
		lis, err := net.Listen("tcp", opt.Addr)
		if err != nil {
			return fmt.Errorf("grpcserver: listen %s: %w", opt.Addr, err)
		}
		s := grpc.NewServer(grpcServerOptions()...)
		rbacv1.RegisterRbacServiceServer(s, svc)
		start(s, lis)
	}
	if opt.TLSAddr != "" {
		lis, err := net.Listen("tcp", opt.TLSAddr)
		if err != nil {
			for _, s := range servers {
				s.Stop()
			}
			return fmt.Errorf("grpcserver: listen TLS %s: %w", opt.TLSAddr, err)
		}
		creds := credentials.NewServerTLSFromCert(opt.TLSCert)
		s := grpc.NewServer(grpcServerOptions(grpc.Creds(creds))...)
		rbacv1.RegisterRbacServiceServer(s, svc)
		start(s, lis)
	}

	select {
	case <-ctx.Done():
		for _, s := range servers {
			s.GracefulStop()
		}
		wg.Wait()
		return nil
	case err := <-errCh:
		for _, s := range servers {
			s.Stop()
		}
		wg.Wait()
		if err == nil || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}
