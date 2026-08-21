package runtime_test

import (
	"context"
	"net/http"

	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/pkg/runtime"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PrepareConfig", func() {
	It("returns error when cfg is nil", func() {
		err := runtime.PrepareConfig(nil)
		Expect(err).To(MatchError("config is required"))
	})

	It("derives pull secret name and default version matrix", func() {
		cfg := &config.Config{
			Registration: config.RegistrationConfig{ProviderName: "acm-cluster-sp"},
			Cluster:      config.ClusterConfig{},
		}

		Expect(runtime.PrepareConfig(cfg)).To(Succeed())
		Expect(cfg.Cluster.PullSecretName).To(Equal("acm-cluster-sp-pull-secret"))
		Expect(cfg.Cluster.VersionMatrix).NotTo(BeEmpty())
	})
})

var _ = Describe("New", func() {
	It("returns error when PullSecretName is empty", func() {
		cfg := &config.Config{
			Registration: config.RegistrationConfig{ProviderName: "acm-cluster-sp"},
			Cluster:      config.ClusterConfig{},
		}

		_, err := runtime.New(context.Background(), cfg, nil, runtime.Options{})
		Expect(err).To(MatchError("cluster pull secret name is empty"))
	})
})

var _ = Describe("LoadConfig embedded", func() {
	It("defaults NATS URL from fallback and provider name", func() {
		GinkgoT().Setenv("SP_CLUSTER_NAMESPACE", "clusters")
		GinkgoT().Setenv("SP_PULL_SECRET", "c2VjcmV0")
		GinkgoT().Setenv("SP_NATS_URL", "")

		cfg, err := runtime.LoadConfig(true, "nats://agent:4222")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Monitoring.NATSUrl).To(Equal("nats://agent:4222"))
		Expect(cfg.Registration.ProviderName).To(Equal("acm-cluster-sp"))
	})

	It("does not require SP_ENDPOINT or DCM_REGISTRATION_URL", func() {
		GinkgoT().Setenv("SP_CLUSTER_NAMESPACE", "clusters")
		GinkgoT().Setenv("SP_PULL_SECRET", "c2VjcmV0")

		_, err := runtime.LoadConfig(true, "nats://agent:4222")
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("MapOperationError", func() {
	It("maps ALREADY_EXISTS to 409", func() {
		opErr := runtime.MapOperationError(service.NewAlreadyExistsError("cluster exists"))
		Expect(opErr).NotTo(BeNil())
		Expect(opErr.StatusCode).To(Equal(http.StatusConflict))
		Expect(opErr.Message).To(Equal("cluster exists"))
	})
})
