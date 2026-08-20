// Package runtime wires the ACM cluster service provider's domain services for
// standalone or embedded use. Standalone callers still attach an HTTP server
// and SPM registration via cmd/acm-cluster-service-provider; embedded callers
// (e.g. environment-agent) use ClusterService and HealthChecker in-process.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster/dispatcher"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/health"
	"github.com/dcm-project/acm-cluster-service-provider/internal/monitoring"
	"github.com/dcm-project/acm-cluster-service-provider/internal/registration"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	spmclient "github.com/dcm-project/service-provider-manager/pkg/client/provider"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Runtime holds wired domain services and optional background workers.
type Runtime struct {
	cfg            *config.Config
	clusterService service.ClusterService
	healthChecker  service.HealthChecker
	monitor        *monitoring.StatusMonitor
	publisher      *monitoring.NATSPublisher
	registrar      *registration.Registrar
	logger         *slog.Logger
}

// PrepareConfig loads derived configuration values that are not set directly
// from environment variables: the OCP→K8s compatibility matrix and the shared
// pull secret name.
func PrepareConfig(cfg *config.Config) error {
	matrix, err := registration.LoadCompatibilityMatrix(cfg.Cluster.VersionMatrixPath)
	if err != nil {
		return fmt.Errorf("loading compatibility matrix: %w", err)
	}
	cfg.Cluster.VersionMatrix = map[string]string(matrix)
	cfg.Cluster.PullSecretName = cfg.Registration.ProviderName + "-pull-secret"
	return nil
}

// New constructs a Runtime: Kubernetes clients, cluster service, health checker,
// and optionally SPM registration and the status monitor. cfg must already have
// passed config.Load (or equivalent) and PrepareConfig.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger, opts Options) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	scheme, err := util.BuildScheme()
	if err != nil {
		return nil, fmt.Errorf("building scheme: %w", err)
	}

	k8sClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	if err := cluster.EnsurePullSecret(ctx, k8sClient, cfg.Cluster, logger); err != nil {
		return nil, fmt.Errorf("ensuring pull secret: %w", err)
	}

	rt := &Runtime{
		cfg:            cfg,
		clusterService: dispatcher.New(k8sClient, cfg.Cluster, cfg.Health.EnabledPlatforms),
		healthChecker:  health.NewChecker(k8sClient, cfg.Health, opts.version(), time.Now()),
		logger:         logger,
	}

	if err := rt.initRegistration(k8sClient, opts); err != nil {
		return nil, err
	}
	if err := rt.initMonitor(restCfg, opts); err != nil {
		_ = rt.Close()
		return nil, err
	}

	return rt, nil
}

func (r *Runtime) initRegistration(k8sClient client.Client, opts Options) error {
	if opts.DisableRegistration {
		return nil
	}

	dcmClient, err := spmclient.NewClientWithResponses(r.cfg.Registration.DCMRegistrationURL)
	if err != nil {
		return fmt.Errorf("creating DCM client: %w", err)
	}

	matrix := registration.CompatibilityMatrix(r.cfg.Cluster.VersionMatrix)
	r.registrar = registration.New(r.cfg.Registration, dcmClient, k8sClient, r.logger, matrix)
	return nil
}

func (r *Runtime) initMonitor(restCfg *rest.Config, opts Options) error {
	if opts.DisableMonitor {
		return nil
	}

	dynamicClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("creating dynamic kubernetes client: %w", err)
	}

	publisher, err := monitoring.NewNATSPublisher(
		r.cfg.Monitoring.NATSUrl,
		r.cfg.Registration.ProviderName,
		r.logger,
	)
	if err != nil {
		return fmt.Errorf("creating NATS publisher: %w", err)
	}
	r.publisher = publisher

	monitorCfg := monitoring.MonitorConfig{
		Namespace:            r.cfg.Cluster.ClusterNamespace,
		ProviderName:         r.cfg.Registration.ProviderName,
		DebounceInterval:     r.cfg.Monitoring.DebounceInterval,
		ResyncInterval:       r.cfg.Monitoring.ResyncInterval,
		PublishRetryMax:      r.cfg.Monitoring.PublishRetryMax,
		PublishRetryInterval: r.cfg.Monitoring.PublishRetryInterval,
	}
	r.monitor = monitoring.New(dynamicClient, monitorCfg, publisher, r.logger)
	return nil
}

// Config returns the runtime configuration. Callers must not mutate it.
func (r *Runtime) Config() *config.Config {
	return r.cfg
}

// ClusterService returns the in-process cluster lifecycle service.
func (r *Runtime) ClusterService() service.ClusterService {
	return r.clusterService
}

// HealthChecker returns the dependency health checker.
func (r *Runtime) HealthChecker() service.HealthChecker {
	return r.healthChecker
}

// Start launches background workers (SPM registration and status monitor).
// It is non-blocking and safe to call once after construction.
func (r *Runtime) Start(ctx context.Context) {
	if r.registrar != nil {
		r.registrar.Start(ctx)
	}
	if r.monitor != nil {
		go func() {
			if err := r.monitor.Start(ctx); err != nil {
				r.logger.Error("status monitor failed", "error", err)
			}
		}()
	}
}

// Close releases runtime resources such as the NATS connection.
func (r *Runtime) Close() error {
	if r.publisher == nil {
		return nil
	}
	return r.publisher.Close()
}
