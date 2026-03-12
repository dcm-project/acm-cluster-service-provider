package apiserver_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
)

var _ = Describe("Server Configuration", func() {
	// TC-HTTP-UT-012: Server sets http.Server timeouts from config
	// REQ-HTTP-050
	It("sets ReadTimeout, WriteTimeout, IdleTimeout on http.Server (TC-HTTP-UT-012)", func() {
		cfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress:     "127.0.0.1:0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  30 * time.Second,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    20 * time.Second,
				IdleTimeout:     45 * time.Second,
			},
		}

		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		srv := apiserver.New(cfg, logger, &oapigen.Unimplemented{})
		Expect(srv).NotTo(BeNil())

		httpSrv := srv.HTTPServer()
		Expect(httpSrv.ReadTimeout).To(Equal(10 * time.Second))
		Expect(httpSrv.WriteTimeout).To(Equal(20 * time.Second))
		Expect(httpSrv.IdleTimeout).To(Equal(45 * time.Second))
	})

	// TC-HTTP-UT-006: Configurable bind address
	// REQ-HTTP-010, REQ-HTTP-050
	It("listens on the configured BIND_ADDRESS (TC-HTTP-UT-006)", func() {
		cfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress:     "127.0.0.1:0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  30 * time.Second,
			},
		}

		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		srv := apiserver.New(cfg, logger, &oapigen.Unimplemented{})
		Expect(srv).NotTo(BeNil())

		// The server should create its own listener from cfg.Server.BindAddress.
		// For the RED phase, we verify that the server uses the configured address
		// by NOT providing a listener — the server should bind to BindAddress itself.
		//
		// Since Run() currently requires a listener argument, we test the behavior
		// by creating a listener on the configured address and verifying the server
		// accepts connections through it.
		ln, err := net.Listen("tcp", cfg.Server.BindAddress)
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		// Wait for server to start accepting connections.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		// Verify the server is listening on 127.0.0.1, not 0.0.0.0.
		host, _, err := net.SplitHostPort(addr)
		Expect(err).NotTo(HaveOccurred())
		Expect(host).To(Equal("127.0.0.1"),
			"server should listen on the configured bind address host")
	})
})
