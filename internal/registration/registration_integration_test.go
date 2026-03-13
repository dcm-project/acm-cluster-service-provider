package registration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	spmv1alpha1 "github.com/dcm-project/service-provider-manager/api/v1alpha1"
	spmclient "github.com/dcm-project/service-provider-manager/pkg/client"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/registration"
)

var _ = Describe("Registration Integration", func() {
	var (
		mockDCMServer *httptest.Server
		logBuf        *syncBuffer
	)

	AfterEach(func() {
		if mockDCMServer != nil {
			mockDCMServer.Close()
			mockDCMServer = nil
		}
	})

	// -------------------------------------------------------------------
	// TC-REG-IT-004: Server accepts requests before registration completes
	// -------------------------------------------------------------------
	It("serves HTTP requests before registration completes (TC-REG-IT-004)", func() {
		var registrationReceived atomic.Bool

		// Mock DCM registry that takes a long time to respond.
		mockDCMServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				// Simulate slow DCM registry.
				time.Sleep(3 * time.Second)
				registrationReceived.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(spmv1alpha1.Provider{
				Name: "acm-cluster-sp", Endpoint: "https://my-sp.example.com/api/v1alpha1/clusters",
				ServiceType: "cluster", SchemaVersion: "v1alpha1",
			})
		}))

		k8sClient := newFakeK8sClient().Build()

		regCfg := defaultRegistrationConfig(mockDCMServer.URL)
		dcmClient, err := spmclient.NewClientWithResponses(mockDCMServer.URL)
		Expect(err).NotTo(HaveOccurred())

		logBuf = &syncBuffer{}
		logger := slog.New(slog.NewJSONHandler(logBuf, nil))

		// Create HTTP server.
		srvCfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress:     ":0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  30 * time.Second,
			},
			Registration: regCfg,
		}
		srv := apiserver.New(srvCfg, logger, &oapigen.Unimplemented{})

		ln, listenErr := net.Listen("tcp", ":0")
		Expect(listenErr).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		// Wait for the HTTP server to be ready.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).Should(Succeed(),
			"HTTP server should be ready before registration completes")

		// Start registration AFTER server is ready (simulating WithOnReady hook).
		reg := registration.New(regCfg, dcmClient, k8sClient, logger)

		// RED: Start() is a no-op, so registration never happens.
		reg.Start(ctx)

		// Verify HTTP server is serving while registration is in progress.
		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr))
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		Expect(resp.StatusCode).NotTo(Equal(http.StatusServiceUnavailable),
			"server should NOT return 503 while registration is in progress")

		// Registration should eventually complete in the background.
		Eventually(registrationReceived.Load).WithTimeout(10*time.Second).WithPolling(200*time.Millisecond).Should(BeTrue(),
			"registration should complete asynchronously in the background")

		cancel()
		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
	})

	// -------------------------------------------------------------------
	// TC-REG-IT-005: Registration failure does not cause SP exit
	// -------------------------------------------------------------------
	It("SP continues serving after registration failure (TC-REG-IT-005)", func() {
		// Mock DCM registry that always fails.
		mockDCMServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(spmv1alpha1.Error{
				Title: "Internal Server Error",
				Type:  "INTERNAL",
			})
		}))

		k8sClient := newFakeK8sClient().Build()

		regCfg := defaultRegistrationConfig(mockDCMServer.URL)
		regCfg.RegistrationInitialBackoff = 20 * time.Millisecond

		dcmClient, err := spmclient.NewClientWithResponses(mockDCMServer.URL)
		Expect(err).NotTo(HaveOccurred())

		logBuf = &syncBuffer{}
		logger := slog.New(slog.NewJSONHandler(logBuf, nil))

		// Create and start HTTP server.
		srvCfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress:     ":0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  30 * time.Second,
			},
			Registration: regCfg,
		}
		srv := apiserver.New(srvCfg, logger, &oapigen.Unimplemented{})

		ln, listenErr := net.Listen("tcp", ":0")
		Expect(listenErr).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		// Wait for server readiness.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		// Start registration (which will fail and retry indefinitely).
		reg := registration.New(regCfg, dcmClient, k8sClient, logger)
		reg.Start(ctx)

		// Wait for at least one retry to be logged.
		Eventually(logBuf.String).WithTimeout(2*time.Second).WithPolling(50*time.Millisecond).Should(
			ContainSubstring("retry"),
			"registration retries should be logged",
		)

		// Verify the SP process is still alive and serving.
		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr))
		Expect(err).NotTo(HaveOccurred(),
			"HTTP server should still be reachable during registration retries")
		_ = resp.Body.Close()

		cancel()
		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
	})

	// -------------------------------------------------------------------
	// TC-REG-IT-006: Registration retries with exponential backoff and max cap
	// -------------------------------------------------------------------
	It("retries with exponential backoff, capped at max interval (TC-REG-IT-006)", func() {
		var requestCount atomic.Int32

		mockDCMServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestCount.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(spmv1alpha1.Error{
				Title: "Internal Server Error",
				Type:  "INTERNAL",
			})
		}))

		k8sClient := newFakeK8sClient().Build()

		regCfg := defaultRegistrationConfig(mockDCMServer.URL)
		regCfg.RegistrationInitialBackoff = 20 * time.Millisecond

		dcmClient, err := spmclient.NewClientWithResponses(mockDCMServer.URL)
		Expect(err).NotTo(HaveOccurred())

		logBuf = &syncBuffer{}
		logger := slog.New(slog.NewJSONHandler(logBuf, nil))
		reg := registration.New(regCfg, dcmClient, k8sClient, logger)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		reg.Start(ctx)

		// Wait for at least 4 retry attempts.
		Eventually(requestCount.Load).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).Should(
			BeNumerically(">=", 4),
			"Start() should retry registration at least 4 times",
		)

		// Parse logged retry_in values to verify exponential backoff.
		// slog JSONHandler serializes time.Duration as nanoseconds (int64).
		type backoffLogEntry struct {
			Msg     string  `json:"msg"`
			RetryIn float64 `json:"retry_in"`
		}

		var backoffs []time.Duration
		for _, line := range strings.Split(logBuf.String(), "\n") {
			if line == "" {
				continue
			}
			var entry backoffLogEntry
			if json.Unmarshal([]byte(line), &entry) != nil {
				continue
			}
			if entry.Msg != "registration failed, will retry" || entry.RetryIn == 0 {
				continue
			}
			backoffs = append(backoffs, time.Duration(entry.RetryIn))
		}

		Expect(len(backoffs)).To(BeNumerically(">=", 3), "need at least 3 retry_in values")
		for i, d := range backoffs {
			Expect(d).To(BeNumerically(">=", regCfg.RegistrationInitialBackoff),
				"backoff[%d] = %v should be >= initial backoff %v", i, d, regCfg.RegistrationInitialBackoff)
			Expect(d).To(BeNumerically("<=", regCfg.RegistrationMaxBackoff),
				"backoff[%d] = %v should be <= max backoff %v", i, d, regCfg.RegistrationMaxBackoff)
			if i > 0 {
				Expect(d).To(BeNumerically(">=", backoffs[i-1]),
					"backoff[%d] = %v should be >= backoff[%d] = %v (non-decreasing)",
					i, d, i-1, backoffs[i-1])
			}
		}
	})

	// -------------------------------------------------------------------
	// TC-REG-IT-007: Registration uses DCM client library
	// -------------------------------------------------------------------
	It("sends registration via DCM client library (TC-REG-IT-007)", func() {
		var receivedBody []byte
		var requestReceived atomic.Bool

		mockDCMServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				defer func() { _ = r.Body.Close() }()
				receivedBody, _ = io.ReadAll(r.Body)
				requestReceived.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(spmv1alpha1.Provider{
				Name: "acm-cluster-sp", Endpoint: "https://my-sp.example.com/api/v1alpha1/clusters",
				ServiceType: "cluster", SchemaVersion: "v1alpha1",
			})
		}))

		k8sClient := newFakeK8sClient().Build()

		regCfg := defaultRegistrationConfig(mockDCMServer.URL)
		dcmClient, err := spmclient.NewClientWithResponses(mockDCMServer.URL)
		Expect(err).NotTo(HaveOccurred())

		logBuf = &syncBuffer{}
		logger := slog.New(slog.NewJSONHandler(logBuf, nil))
		reg := registration.New(regCfg, dcmClient, k8sClient, logger)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// RED: Start() is a no-op, so no request is ever sent.
		reg.Start(ctx)

		// Verify the DCM client library sent a well-formed request.
		Eventually(requestReceived.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"registration request should arrive at mock DCM server via DCM client library")

		// Verify the body is valid JSON with expected DCM client library structure.
		var parsed map[string]interface{}
		Expect(json.Unmarshal(receivedBody, &parsed)).To(Succeed(),
			"request body should be valid JSON")
		Expect(parsed).To(HaveKey("name"))
		Expect(parsed).To(HaveKey("service_type"))
		Expect(parsed).To(HaveKey("endpoint"))
		Expect(parsed).To(HaveKey("schema_version"))
	})
})
