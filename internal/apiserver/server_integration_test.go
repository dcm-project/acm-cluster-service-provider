package apiserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
)

// syncBuffer is a goroutine-safe bytes.Buffer for capturing log output
// shared between the server goroutine (writer) and the test goroutine (reader).
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// panicOnListHandler implements ServerInterface, panicking on ListClusters
// to test recovery middleware.
type panicOnListHandler struct {
	oapigen.Unimplemented
}

func (p *panicOnListHandler) ListClusters(w http.ResponseWriter, _ *http.Request, _ oapigen.ListClustersParams) {
	panic("unexpected failure")
}

// failingStrictHandler implements StrictServerInterface, returning an error
// from ListClusters to test response error handling (REQ-HTTP-091).
type failingStrictHandler struct{}

func (f *failingStrictHandler) ListClusters(_ context.Context, _ oapigen.ListClustersRequestObject) (oapigen.ListClustersResponseObject, error) {
	return nil, fmt.Errorf("internal failure")
}

func (f *failingStrictHandler) CreateCluster(_ context.Context, _ oapigen.CreateClusterRequestObject) (oapigen.CreateClusterResponseObject, error) {
	return nil, nil
}

func (f *failingStrictHandler) DeleteCluster(_ context.Context, _ oapigen.DeleteClusterRequestObject) (oapigen.DeleteClusterResponseObject, error) {
	return nil, nil
}

func (f *failingStrictHandler) GetCluster(_ context.Context, _ oapigen.GetClusterRequestObject) (oapigen.GetClusterResponseObject, error) {
	return nil, nil
}

func (f *failingStrictHandler) GetHealth(_ context.Context, _ oapigen.GetHealthRequestObject) (oapigen.GetHealthResponseObject, error) {
	return nil, nil
}

var _ = Describe("HTTP Server", func() {

	// startServer creates a server with the given config, starts it in a
	// goroutine, and returns the address, cancel/cleanup functions.
	//
	// When signals are non-nil, the context is wired to those OS signals
	// via signal.NotifyContext so the server shuts down on signal delivery.
	// When signals is nil, a plain context.WithCancel is used.
	startServer := func(
		cfg *config.Config,
		logBuf *syncBuffer,
		signals []os.Signal,
		handler oapigen.ServerInterface,
		wrappers ...func(http.Handler) http.Handler,
	) (
		addr string,
		cancel context.CancelFunc,
		errCh chan error,
	) {
		var logger *slog.Logger
		if logBuf != nil {
			logger = slog.New(slog.NewJSONHandler(logBuf, nil))
		} else {
			logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		}

		if handler == nil {
			handler = &oapigen.Unimplemented{}
		}

		srv := apiserver.New(cfg, logger, handler)
		Expect(srv).NotTo(BeNil(), "New() must return a non-nil server")

		for _, w := range wrappers {
			srv.WrapHandler(w)
		}

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr = ln.Addr().String()

		var ctx context.Context
		if len(signals) > 0 {
			signal.Reset(signals...)
			ctx, cancel = signal.NotifyContext(context.Background(), signals...)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}

		errCh = make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		// Wait for the server to start handling requests.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/", addr))
			if reqErr != nil {
				return reqErr
			}
			resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		return addr, cancel, errCh
	}

	defaultConfig := func() *config.Config {
		return &config.Config{
			Server: config.ServerConfig{
				BindAddress:     ":0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  30 * time.Second,
			},
		}
	}

	// TC-HTTP-IT-001: Routes mounted under correct paths
	// REQ-HTTP-020
	It("mounts routes under correct API paths (TC-HTTP-IT-001)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		baseURL := fmt.Sprintf("http://%s", addr)

		// GET /clusters (no prefix) should return 404.
		resp, err := http.Get(baseURL + "/clusters")
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound),
			"/clusters without prefix should be 404")

		// GET /api/v1alpha1/clusters should NOT return 404.
		resp, err = http.Get(baseURL + "/api/v1alpha1/clusters")
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).NotTo(Equal(http.StatusNotFound),
			"/api/v1alpha1/clusters should be a valid route")
		Expect(resp.StatusCode).NotTo(Equal(http.StatusMethodNotAllowed),
			"/api/v1alpha1/clusters GET should not be 405")

		// GET /api/v1alpha1/clusters/health should NOT return 404.
		resp, err = http.Get(baseURL + "/api/v1alpha1/clusters/health")
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).NotTo(Equal(http.StatusNotFound),
			"/api/v1alpha1/clusters/health should be a valid route")
	})

	// TC-HTTP-IT-002: Graceful shutdown drains in-flight requests
	// REQ-HTTP-030, REQ-HTTP-040
	It("drains in-flight requests on SIGTERM (TC-HTTP-IT-002)", Serial, func() {
		reqStarted := make(chan struct{})
		reqRelease := make(chan struct{})

		slowWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/slow" {
					close(reqStarted)
					<-reqRelease
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(defaultConfig(), nil, []os.Signal{syscall.SIGTERM}, nil, slowWrapper)
		defer cancel()

		type result struct {
			resp *http.Response
			err  error
		}
		respCh := make(chan result, 1)
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/test/slow", addr))
			respCh <- result{resp, err}
		}()

		<-reqStarted

		proc, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		Expect(proc.Signal(syscall.SIGTERM)).To(Succeed())

		close(reqRelease)

		var res result
		Eventually(respCh).WithTimeout(5 * time.Second).Should(Receive(&res))
		Expect(res.err).NotTo(HaveOccurred())
		defer res.resp.Body.Close()
		Expect(res.resp.StatusCode).To(Equal(http.StatusOK))

		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive(BeNil()))

		_, err = http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr))
		Expect(err).To(HaveOccurred())
	})

	// TC-HTTP-IT-003: Panic recovery keeps server alive
	// REQ-HTTP-070
	It("recovers from handler panic and returns RFC 7807 500 (TC-HTTP-IT-003)", func() {
		h := &panicOnListHandler{}
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil, h)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		// Hit the panicking route.
		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters", addr))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Header.Get("Content-Type")).To(Equal("application/problem+json"))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var problemJSON map[string]any
		Expect(json.Unmarshal(body, &problemJSON)).To(Succeed())
		Expect(problemJSON).To(HaveKeyWithValue("type", "INTERNAL"))
		Expect(problemJSON["status"]).To(BeNumerically("==", 500))

		// Server should still be alive after the panic.
		resp2, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr))
		Expect(err).NotTo(HaveOccurred())
		resp2.Body.Close()
	})

	// TC-HTTP-IT-004: Request errors return RFC 7807
	// REQ-HTTP-090
	It("returns RFC 7807 for malformed request body (TC-HTTP-IT-004)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		// Send POST with malformed JSON body.
		resp, err := http.Post(
			fmt.Sprintf("http://%s/api/v1alpha1/clusters", addr),
			"application/json",
			strings.NewReader("{invalid json"),
		)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.Header.Get("Content-Type")).To(Equal("application/problem+json"))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var problemJSON map[string]any
		Expect(json.Unmarshal(body, &problemJSON)).To(Succeed())
		Expect(problemJSON).To(HaveKey("type"))
		Expect(problemJSON).To(HaveKey("title"))
		Expect(problemJSON).To(HaveKey("status"))
	})

	// TC-HTTP-IT-005: Response errors return RFC 7807 with type=INTERNAL
	// REQ-HTTP-091
	It("returns RFC 7807 with type=INTERNAL for response errors (TC-HTTP-IT-005)", func() {
		// Use a strict handler that returns an error from ListClusters.
		strictHandler := &failingStrictHandler{}
		wrappedHandler := oapigen.NewStrictHandlerWithOptions(strictHandler, nil, oapigen.StrictHTTPServerOptions{})

		addr, cancel, errCh := startServer(defaultConfig(), nil, nil, wrappedHandler)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters", addr))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.Header.Get("Content-Type")).To(Equal("application/problem+json"))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var problemJSON map[string]any
		Expect(json.Unmarshal(body, &problemJSON)).To(Succeed())
		Expect(problemJSON).To(HaveKeyWithValue("type", "INTERNAL"))

		// Must not leak internal details.
		bodyStr := string(body)
		Expect(bodyStr).NotTo(ContainSubstring("internal failure"),
			"internal error message must not leak to client")
		Expect(bodyStr).NotTo(ContainSubstring("goroutine"),
			"goroutine info must not leak")
	})

	// TC-HTTP-IT-008: Request timeout middleware
	// REQ-HTTP-110
	It("cancels requests exceeding REQUEST_TIMEOUT (TC-HTTP-IT-008)", func() {
		shortTimeoutCfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress:     ":0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  1 * time.Second,
			},
		}

		handlerDone := make(chan time.Duration, 1)

		slowWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/slow" {
					start := time.Now()
					// Wait for context cancellation (timeout middleware)
					// or a hard deadline to prevent test from blocking forever.
					select {
					case <-r.Context().Done():
						handlerDone <- time.Since(start)
						http.Error(w, "timeout", http.StatusGatewayTimeout)
					case <-time.After(5 * time.Second):
						handlerDone <- time.Since(start)
						http.Error(w, "no timeout applied", http.StatusOK)
					}
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(shortTimeoutCfg, nil, nil, nil, slowWrapper)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://%s/test/slow", addr))
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()

		// The handler should have been cancelled by the timeout middleware
		// within ~1s (the REQUEST_TIMEOUT). Without middleware, it will
		// complete after 5s via the fallback timer.
		var elapsed time.Duration
		Eventually(handlerDone).WithTimeout(10 * time.Second).Should(Receive(&elapsed))
		Expect(elapsed).To(BeNumerically("<", 2*time.Second),
			"request should be cancelled by timeout middleware within ~1s, but took %s", elapsed)
	})

	// TC-HTTP-IT-009: Shutdown timeout expiry force-closes connections
	// REQ-HTTP-030, REQ-HTTP-040
	It("force-terminates when shutdown timeout expires (TC-HTTP-IT-009)", func() {
		shortTimeoutCfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress:     ":0",
				ShutdownTimeout: 200 * time.Millisecond,
				RequestTimeout:  30 * time.Second,
			},
		}

		reqStarted := make(chan struct{})

		blockingWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/block" {
					close(reqStarted)
					<-r.Context().Done()
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(shortTimeoutCfg, nil, nil, nil, blockingWrapper)

		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/test/block", addr))
			if err == nil {
				resp.Body.Close()
			}
		}()

		<-reqStarted

		cancel()

		// Server should exit within the shutdown timeout + buffer,
		// returning context.DeadlineExceeded.
		var serverErr error
		Eventually(errCh).WithTimeout(2 * time.Second).Should(Receive(&serverErr))
		Expect(serverErr).To(MatchError(context.DeadlineExceeded))
	})

	// TC-HTTP-IT-010: Request logging middleware
	// REQ-HTTP-060
	It("logs request method, path, status, and duration (TC-HTTP-IT-010)", func() {
		var logBuf syncBuffer
		addr, cancel, errCh := startServer(defaultConfig(), &logBuf, nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/clusters", addr))
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()

		// Poll the log buffer until the logging middleware has written.
		Eventually(func() string {
			return logBuf.String()
		}).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).Should(
			ContainSubstring("request completed"),
			"log should contain the request completed entry",
		)

		logOutput := logBuf.String()

		Expect(logOutput).To(ContainSubstring("GET"),
			"log should contain the HTTP method")
		Expect(logOutput).To(ContainSubstring("/api/v1alpha1/clusters"),
			"log should contain the request path")
		// Check for status code and duration fields in log output.
		Expect(logOutput).To(SatisfyAny(
			ContainSubstring("status"),
			ContainSubstring("code"),
		), "log should contain status code field")
		Expect(logOutput).To(SatisfyAny(
			ContainSubstring("duration"),
			ContainSubstring("latency"),
			ContainSubstring("elapsed"),
		), "log should contain duration field")
	})

	// TC-HTTP-IT-011: Lifecycle events logged
	// REQ-HTTP-080
	It("logs lifecycle events (startup and shutdown) (TC-HTTP-IT-011)", func() {
		var logBuf syncBuffer
		addr, cancel, errCh := startServer(defaultConfig(), &logBuf, nil, nil)

		Expect(addr).NotTo(BeEmpty())

		// Check startup log.
		Expect(logBuf.String()).To(ContainSubstring(addr),
			"startup log should contain the listen address")

		// Trigger shutdown.
		cancel()
		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())

		logOutput := logBuf.String()
		Expect(logOutput).To(SatisfyAny(
			ContainSubstring("shutdown"),
			ContainSubstring("shutting down"),
			ContainSubstring("stopping"),
		), "log should contain shutdown event")
	})
})
