package runtime

import (
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PrepareConfig", func() {
	It("derives pull secret name and default version matrix", func() {
		cfg := &config.Config{
			Registration: config.RegistrationConfig{ProviderName: "acm-cluster-sp"},
			Cluster:      config.ClusterConfig{},
		}

		Expect(PrepareConfig(cfg)).To(Succeed())
		Expect(cfg.Cluster.PullSecretName).To(Equal("acm-cluster-sp-pull-secret"))
		Expect(cfg.Cluster.VersionMatrix).NotTo(BeEmpty())
	})
})

var _ = Describe("Options", func() {
	It("defaults version when unset", func() {
		Expect(Options{}.version()).To(Equal("0.0.1-dev"))
	})

	It("uses explicit version when set", func() {
		Expect(Options{Version: "1.2.3"}.version()).To(Equal("1.2.3"))
	})
})
