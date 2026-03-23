package baremetal_test

import (
	"context"
	"errors"
	"fmt"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("BareMetal Service", func() {
	var (
		ctx context.Context
		cfg config.ClusterConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg = defaultConfig()
	})

	// ── Create ──────────────────────────────────────────────────────────

	Describe("Create", func() {
		It("TC-BM-UT-001: creates HostedCluster + NodePool with Agent platform, InfraEnv, labels, and replicas", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Nodes.Workers.Count = 3

			result, err := svc.Create(ctx, "bm-id", req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Verify HostedCluster
			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			hc := hcList.Items[0]
			Expect(hc.Spec.Platform.Type).To(Equal(hyperv1.AgentPlatform))
			Expect(hc.Spec.Platform.Agent).NotTo(BeNil())
			Expect(hc.Spec.Platform.Agent.AgentNamespace).To(Equal("agent-ns"))
			Expect(hc.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "dcm"))
			Expect(hc.Labels).To(HaveKeyWithValue("dcm-instance-id", "bm-id"))
			Expect(hc.Labels).To(HaveKeyWithValue("dcm-service-type", "cluster"))

			// Verify NodePool
			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			np := npList.Items[0]
			Expect(np.Spec.Platform.Type).To(Equal(hyperv1.AgentPlatform))
			Expect(np.Spec.Replicas).NotTo(BeNil())
			Expect(*np.Spec.Replicas).To(Equal(int32(3)))
			Expect(np.Spec.Platform.Agent).NotTo(BeNil())
			Expect(np.Spec.Platform.Agent.AgentLabelSelector).NotTo(BeNil())
			Expect(np.Spec.Platform.Agent.AgentLabelSelector.MatchLabels).To(
				HaveKeyWithValue("infraenvs.agent-install.openshift.io", "my-infra"),
			)
			Expect(np.Labels).To(HaveKeyWithValue("dcm-instance-id", "bm-id"))
		})

		It("TC-BM-UT-002: agent labels merged into NodePool AgentLabelSelector", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.ProviderHints.Acm.AgentLabels = &map[string]string{
				"location": "dc1",
				"rack":     "r1",
			}

			result, err := svc.Create(ctx, "bm-id", req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			np := npList.Items[0]
			Expect(np.Spec.Platform.Agent).NotTo(BeNil())
			Expect(np.Spec.Platform.Agent.AgentLabelSelector).NotTo(BeNil())
			matchLabels := np.Spec.Platform.Agent.AgentLabelSelector.MatchLabels
			Expect(matchLabels).To(HaveKeyWithValue("infraenvs.agent-install.openshift.io", "my-infra"))
			Expect(matchLabels).To(HaveKeyWithValue("location", "dc1"))
			Expect(matchLabels).To(HaveKeyWithValue("rack", "r1"))
		})

		It("TC-BM-UT-003: missing infra_env and no default returns error", func() {
			cfgNoDefault := cfg
			cfgNoDefault.DefaultInfraEnv = ""
			svc, _ := newTestService(cfgNoDefault)
			req := validCreateCluster()
			req.Spec.ProviderHints.Acm.InfraEnv = nil

			_, err := svc.Create(ctx, "bm-id", req)

			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(errors.As(err, &domainErr)).To(BeTrue())
		})

		It("TC-BM-UT-004: missing infra_env uses SP_DEFAULT_INFRA_ENV", func() {
			cfgWithDefault := cfg
			cfgWithDefault.DefaultInfraEnv = "default-infra"
			svc, k8s := newTestService(cfgWithDefault)
			req := validCreateCluster()
			req.Spec.ProviderHints.Acm.InfraEnv = nil

			result, err := svc.Create(ctx, "bm-id", req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			np := npList.Items[0]
			Expect(np.Spec.Platform.Agent).NotTo(BeNil())
			Expect(np.Spec.Platform.Agent.AgentLabelSelector).NotTo(BeNil())
			Expect(np.Spec.Platform.Agent.AgentLabelSelector.MatchLabels).To(
				HaveKeyWithValue("infraenvs.agent-install.openshift.io", "default-infra"),
			)
		})

		It("TC-BM-UT-005: worker resources are informational — no resource constraints on NodePool", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Nodes.Workers.Cpu = 8
			req.Spec.Nodes.Workers.Memory = "32GB"
			req.Spec.Nodes.Workers.Storage = "500GB"

			result, err := svc.Create(ctx, "bm-id", req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			np := npList.Items[0]
			Expect(np.Spec.Replicas).NotTo(BeNil())
			// Agent platform NodePool must NOT have KubeVirt resource constraints
			Expect(np.Spec.Platform.Kubevirt).To(BeNil())
		})

		It("TC-BM-UT-008: NodePool creation failure triggers HostedCluster rollback", func() {
			svc, k8s := newTestServiceWithInterceptor(cfg, nil, interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*hyperv1.NodePool); ok {
						return fmt.Errorf("simulated NP failure")
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			req := validCreateCluster()

			_, err := svc.Create(ctx, "bm-id", req)

			Expect(err).To(HaveOccurred())

			// Verify orphan HC was cleaned up
			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(BeEmpty())
		})

		It("TC-BM-UT-010: base_domain from request overrides config; config used as fallback", func() {
			cfgWithDomain := cfg
			cfgWithDomain.BaseDomain = "cluster.local"
			svc, k8s := newTestService(cfgWithDomain)

			// Case 1: Request overrides config
			req := validCreateCluster()
			req.Spec.ProviderHints.Acm.BaseDomain = util.Ptr("example.com")
			result, err := svc.Create(ctx, "bm-id-1", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			Expect(hcList.Items[0].Spec.DNS.BaseDomain).To(Equal("example.com"))

			// Case 2: Falls back to config default
			svc2, k8s2 := newTestService(cfgWithDomain)
			req2 := validCreateCluster()
			req2.Spec.Metadata.Name = "test-cluster-2"
			result2, err2 := svc2.Create(ctx, "bm-id-2", req2)
			Expect(err2).NotTo(HaveOccurred())
			Expect(result2).NotTo(BeNil())

			var hcList2 hyperv1.HostedClusterList
			Expect(k8s2.List(ctx, &hcList2, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList2.Items).To(HaveLen(1))
			Expect(hcList2.Items[0].Spec.DNS.BaseDomain).To(Equal("cluster.local"))
		})

		It("TC-BM-UT-011: no base_domain and no SP_BASE_DOMAIN returns error", func() {
			cfgNoDomain := cfg
			cfgNoDomain.BaseDomain = ""
			svc, _ := newTestService(cfgNoDomain)
			req := validCreateCluster()
			req.Spec.ProviderHints = &v1alpha1.ProviderHints{
				Acm: &v1alpha1.ACMProviderHints{
					Platform: req.Spec.ProviderHints.Acm.Platform,
					InfraEnv: req.Spec.ProviderHints.Acm.InfraEnv,
				},
			}

			_, err := svc.Create(ctx, "bm-id", req)

			Expect(err).To(HaveOccurred())
		})

		It("TC-BM-UT-012: release image override bypasses ClusterImageSet lookup", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.ProviderHints.Acm.ReleaseImage = util.Ptr("quay.io/ocp-release:4.15.2-x86_64")

			result, err := svc.Create(ctx, "bm-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			Expect(hcList.Items[0].Spec.Release.Image).To(Equal("quay.io/ocp-release:4.15.2-x86_64"))
		})
	})
})
