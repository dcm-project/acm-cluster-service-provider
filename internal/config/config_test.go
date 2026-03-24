package config_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
)

var _ = Describe("Config", func() {
	// All required env vars needed for a valid Load().
	requiredVars := map[string]string{
		"DCM_REGISTRATION_URL": "http://dcm",
		"SP_ENDPOINT":          "http://sp",
		"SP_CLUSTER_NAMESPACE": "clusters",
	}

	setAllRequired := func() {
		for k, v := range requiredVars {
			GinkgoT().Setenv(k, v)
		}
	}

	DescribeTable("TC-CFG-UT-001: required config missing causes fail-fast",
		func(missingVar string) {
			for k, v := range requiredVars {
				if k != missingVar {
					GinkgoT().Setenv(k, v)
				}
			}
			// Record original value for Ginkgo restore, then unset.
			GinkgoT().Setenv(missingVar, "")
			Expect(os.Unsetenv(missingVar)).To(Succeed())

			_, err := config.Load()
			Expect(err).To(HaveOccurred(), "Load() should fail when %s is missing", missingVar)
		},
		Entry("DCM_REGISTRATION_URL missing", "DCM_REGISTRATION_URL"),
		Entry("SP_ENDPOINT missing", "SP_ENDPOINT"),
		Entry("SP_CLUSTER_NAMESPACE missing", "SP_CLUSTER_NAMESPACE"),
	)

	It("TC-CFG-UT-002: applies defaults when optional vars are not set", func() {
		setAllRequired()

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Server.BindAddress).To(Equal(":8080"))
		Expect(cfg.Server.ShutdownTimeout.String()).To(Equal("15s"))
		Expect(cfg.Registration.ProviderName).To(Equal("acm-cluster-sp"))
		Expect(cfg.Health.EnabledPlatforms).To(Equal([]string{"kubevirt", "baremetal"}))
	})
})
