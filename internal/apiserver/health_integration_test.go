package apiserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	hdlr "github.com/dcm-project/acm-cluster-service-provider/internal/handler"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
)

// mockHealthChecker implements service.HealthChecker for TC-HLT-IT-007.
// Returns a fully populated Health with all required fields.
type mockHealthChecker struct{}

func (m *mockHealthChecker) Check(_ context.Context) v1alpha1.Health {
	uptime := 42
	return v1alpha1.Health{
		Status:  util.Ptr("healthy"),
		Type:    util.Ptr("acm-cluster-service-provider.dcm.io/health"),
		Path:    util.Ptr("health"),
		Version: util.Ptr("v1.0.0"),
		Uptime:  &uptime,
	}
}

var _ = Describe("Health HTTP Endpoint", func() {
	// startHealthServer creates a minimal server with a strict handler,
	// starts it in a goroutine, and returns the address + cleanup.
	startHealthServer := func(handler oapigen.StrictServerInterface) (
		addr string,
		cancel context.CancelFunc,
		errCh chan error,
	) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress:     ":0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  30 * time.Second,
			},
		}

		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		wrappedHandler := oapigen.NewStrictHandlerWithOptions(handler, nil, oapigen.StrictHTTPServerOptions{})
		srv := apiserver.New(cfg, logger, wrappedHandler)
		Expect(srv).NotTo(BeNil())

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr = ln.Addr().String()

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())

		errCh = make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		return addr, cancel, errCh
	}

	// ---------------------------------------------------------------
	// TC-HLT-IT-007: Health endpoint integration (HTTP layer)
	// REQ-HLT-010, REQ-HLT-020
	// ---------------------------------------------------------------
	It("returns 200 with all required health fields (TC-HLT-IT-007)", func() {
		addr, cancel, errCh := startHealthServer(hdlr.New(nil, &mockHealthChecker{}, slog.New(slog.NewTextHandler(io.Discard, nil))))
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()

		// REQ-HLT-010: HTTP 200
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		// REQ-HLT-010: application/json content-type
		Expect(resp.Header.Get("Content-Type")).To(Equal("application/json"))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var healthBody map[string]any
		Expect(json.Unmarshal(body, &healthBody)).To(Succeed())

		// REQ-HLT-020: all 5 required fields present
		Expect(healthBody).To(HaveKey("status"))
		Expect(healthBody).To(HaveKey("type"))
		Expect(healthBody).To(HaveKey("path"))
		Expect(healthBody).To(HaveKey("version"))
		Expect(healthBody).To(HaveKey("uptime"))
	})
})
