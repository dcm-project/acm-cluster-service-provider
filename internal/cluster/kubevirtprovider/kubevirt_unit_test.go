package kubevirtprovider_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("KubeVirt Service", func() {
	var (
		ctx context.Context
		cfg cluster.Config
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg = defaultConfig()
	})

	// ── Create ──────────────────────────────────────────────────────────

	Describe("Create", func() {
		It("TC-KV-UT-001: creates HostedCluster + NodePool with correct platform, labels, and replicas", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Nodes.Workers.Count = 2

			result, err := svc.Create(ctx, "test-id", req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// MF-2: UpdateTime must equal CreateTime on creation
			Expect(result.CreateTime).NotTo(BeNil())
			Expect(result.UpdateTime).NotTo(BeNil())
			Expect(*result.UpdateTime).To(Equal(*result.CreateTime))

			// Verify HostedCluster was created
			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			hc := hcList.Items[0]
			Expect(hc.Spec.Platform.Type).To(Equal(hyperv1.KubevirtPlatform))
			Expect(hc.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "dcm"))
			Expect(hc.Labels).To(HaveKeyWithValue("dcm-instance-id", "test-id"))
			Expect(hc.Labels).To(HaveKeyWithValue("dcm-service-type", "cluster"))

			// Verify NodePool was created
			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			np := npList.Items[0]
			Expect(np.Spec.Replicas).NotTo(BeNil())
			Expect(*np.Spec.Replicas).To(Equal(int32(2)))
		})

		It("TC-KV-UT-002: control_plane.count and storage are ignored", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Nodes.ControlPlane.Count = v1alpha1.N5
			req.Spec.Nodes.ControlPlane.Storage = "500GB"

			result, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			// HC should not have explicit CP replica count or etcd storage config
		})

		It("TC-KV-UT-003: control_plane CPU and memory map to resource requests", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Nodes.ControlPlane.Cpu = 4
			req.Spec.Nodes.ControlPlane.Memory = "16GB"

			result, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			// HC resource requests should include mapped CPU (4) and memory (16GB)
		})

		DescribeTable("TC-KV-UT-004: version validation via compatibility matrix and ClusterImageSets",
			func(version string, expectSuccess bool, _ *v1alpha1.ErrorType) {
				svc, _ := newTestService(cfg)
				req := validCreateCluster()
				req.Spec.Version = version

				result, err := svc.Create(ctx, "test-id", req)

				if expectSuccess {
					Expect(err).NotTo(HaveOccurred())
					Expect(result).NotTo(BeNil())
				} else {
					Expect(err).To(HaveOccurred())
					var domainErr *service.DomainError
					Expect(err).To(BeAssignableToTypeOf(domainErr))
				}
			},
			Entry("valid version 1.28", "1.28", true, nil),
			Entry("no K8s-to-OCP mapping (9.99)", "9.99", false, util.Ptr(v1alpha1.ErrorTypeUNPROCESSABLEENTITY)),
			Entry("partial K8s version (1.3)", "1.3", false, util.Ptr(v1alpha1.ErrorTypeUNPROCESSABLEENTITY)),
			Entry("OCP format rejected (4.17.0)", "4.17.0", false, util.Ptr(v1alpha1.ErrorTypeUNPROCESSABLEENTITY)),
			Entry("valid version 1.30", "1.30", true, nil),
			Entry("no ClusterImageSet for 1.31", "1.31", false, util.Ptr(v1alpha1.ErrorTypeUNPROCESSABLEENTITY)),
		)

		It("TC-KV-UT-005: release image override bypasses ClusterImageSet lookup", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			platform := v1alpha1.Kubevirt
			req.Spec.ProviderHints = &v1alpha1.ProviderHints{
				Acm: &v1alpha1.ACMProviderHints{
					Platform:     &platform,
					ReleaseImage: util.Ptr("quay.io/ocp-release:4.15.2-x86_64"),
				},
			}

			result, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			Expect(hcList.Items[0].Spec.Release.Image).To(Equal("quay.io/ocp-release:4.15.2-x86_64"))
		})

		It("TC-KV-UT-006: base domain from request overrides config default", func() {
			cfgWithDomain := cfg
			cfgWithDomain.BaseDomain = "cluster.local"
			svc, k8s := newTestService(cfgWithDomain)

			// Case 1: Request overrides config
			req := validCreateCluster()
			req.Spec.ProviderHints = &v1alpha1.ProviderHints{
				Acm: &v1alpha1.ACMProviderHints{
					BaseDomain: util.Ptr("example.com"),
				},
			}
			result, err := svc.Create(ctx, "test-id-1", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			Expect(hcList.Items[0].Spec.DNS.BaseDomain).To(Equal("example.com"))

			// Case 2: Falls back to config default (separate test invocation)
			svc2, k8s2 := newTestService(cfgWithDomain)
			req2 := validCreateCluster()
			req2.Spec.Metadata.Name = "test-cluster-2"
			result2, err2 := svc2.Create(ctx, "test-id-2", req2)
			Expect(err2).NotTo(HaveOccurred())
			Expect(result2).NotTo(BeNil())

			var hcList2 hyperv1.HostedClusterList
			Expect(k8s2.List(ctx, &hcList2, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList2.Items).To(HaveLen(1))
			Expect(hcList2.Items[0].Spec.DNS.BaseDomain).To(Equal("cluster.local"))
		})

		It("TC-KV-UT-007: default platform is KubeVirt when no provider_hints", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.ProviderHints = nil

			result, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			Expect(hcList.Items[0].Spec.Platform.Type).To(Equal(hyperv1.KubevirtPlatform))
		})

		It("TC-KV-UT-008: memory/storage format conversion (DCM to K8s)", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Nodes.Workers.Memory = "16GB"
			req.Spec.Nodes.Workers.Storage = "120GB"

			result, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			np := npList.Items[0]
			Expect(np.Spec.Platform.Kubevirt).NotTo(BeNil())
			Expect(np.Spec.Platform.Kubevirt.Compute).NotTo(BeNil())
			// Memory should be 16G equivalent
			Expect(np.Spec.Platform.Kubevirt.Compute.Memory).NotTo(BeNil())
		})

		It("TC-KV-UT-009: workers storage maps to root disk size", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Nodes.Workers.Storage = "120GB"

			result, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			np := npList.Items[0]
			Expect(np.Spec.Platform.Kubevirt).NotTo(BeNil())
			Expect(np.Spec.Platform.Kubevirt.RootVolume).NotTo(BeNil())
			Expect(np.Spec.Platform.Kubevirt.RootVolume.Persistent).NotTo(BeNil())
			Expect(np.Spec.Platform.Kubevirt.RootVolume.Persistent.Size).NotTo(BeNil())
		})

		It("TC-KV-UT-014: duplicate ID returns AlreadyExists error", func() {
			existing := buildHostedCluster("existing", testNamespace, withDCMLabels("abc123"))
			svc, _ := newTestService(cfg, existing)
			req := validCreateCluster()

			_, err := svc.Create(ctx, "abc123", req)

			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-015: duplicate metadata.name returns AlreadyExists with name conflict detail", func() {
			existing := buildHostedCluster("my-cluster", testNamespace)
			svc, _ := newTestService(cfg, existing)
			req := validCreateCluster()
			req.Spec.Metadata.Name = "my-cluster"

			_, err := svc.Create(ctx, "new-id", req)

			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(errors.As(err, &domainErr)).To(BeTrue())
			// SF-2: Error message must indicate this is a name conflict
			Expect(domainErr.Message).To(ContainSubstring("name"))
		})

		It("TC-KV-UT-016: K8s API failure returns internal error without leaking details", func() {
			svc, _ := newTestServiceWithInterceptor(cfg, nil, interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return fmt.Errorf("kube-apiserver: connection refused")
				},
			})
			req := validCreateCluster()

			_, err := svc.Create(ctx, "test-id", req)
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(errors.As(err, &domainErr)).To(BeTrue())
			Expect(domainErr.Message).NotTo(ContainSubstring("kube"))
		})

		It("TC-KV-UT-017: NodePool creation failure triggers HostedCluster rollback", func() {
			svc, k8s := newTestServiceWithInterceptor(cfg, nil, interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*hyperv1.NodePool); ok {
						return fmt.Errorf("simulated NP failure")
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			req := validCreateCluster()

			_, err := svc.Create(ctx, "test-id", req)

			Expect(err).To(HaveOccurred())

			// Verify orphan HC was cleaned up
			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(BeEmpty())
		})

		It("TC-KV-UT-020: missing base_domain returns error", func() {
			cfgNoDomain := cfg
			cfgNoDomain.BaseDomain = ""
			svc, _ := newTestService(cfgNoDomain)
			req := validCreateCluster()
			req.Spec.ProviderHints = nil // No base_domain in request either

			_, err := svc.Create(ctx, "test-id", req)

			Expect(err).To(HaveOccurred())
		})

		It("TC-KV-UT-028: rollback failure returns combined error", func() {
			svc, _ := newTestServiceWithInterceptor(cfg, nil, interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*hyperv1.NodePool); ok {
						return fmt.Errorf("simulated NP failure")
					}
					return c.Create(ctx, obj, opts...)
				},
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("simulated delete failure")
				},
			})
			req := validCreateCluster()

			_, err := svc.Create(ctx, "test-id", req)
			Expect(err).To(HaveOccurred())
			// Error should contain both failures
			Expect(err.Error()).To(ContainSubstring("rollback"))
		})

		It("TC-KV-UT-029: empty platform string defaults to KubeVirt", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			emptyPlatform := v1alpha1.ACMProviderHintsPlatform("")
			req.Spec.ProviderHints = &v1alpha1.ProviderHints{
				Acm: &v1alpha1.ACMProviderHints{
					Platform: &emptyPlatform,
				},
			}

			result, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			Expect(hcList.Items[0].Spec.Platform.Type).To(Equal(hyperv1.KubevirtPlatform))
		})

		// ── Resource Identity (TC-XC-ID-UT-xxx) ─────────────────────────

		It("TC-XC-ID-UT-001: resources have both K8s name and DCM instance ID", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()
			req.Spec.Metadata.Name = "my-cluster"

			result, err := svc.Create(ctx, "dcm-id", req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
			hc := hcList.Items[0]
			Expect(hc.Name).To(Equal("my-cluster"))
			Expect(hc.Labels).To(HaveKeyWithValue("dcm-instance-id", "dcm-id"))

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(HaveLen(1))
			Expect(npList.Items[0].Labels).To(HaveKeyWithValue("dcm-instance-id", "dcm-id"))
		})

		It("TC-XC-ID-UT-002: dcm-instance-id label matches id field for lookup", func() {
			svc, k8s := newTestService(cfg)
			req := validCreateCluster()

			_, err := svc.Create(ctx, "test-id", req)
			Expect(err).NotTo(HaveOccurred())

			// Verify the HC can be found by label
			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{"dcm-instance-id": "test-id"},
			)).To(Succeed())
			Expect(hcList.Items).To(HaveLen(1))
		})
	})

	// ── Get ─────────────────────────────────────────────────────────────

	Describe("Get", func() {
		It("TC-KV-UT-010: READY cluster has api_endpoint, console_uri, kubeconfig, version, and update_time", func() {
			kubeconfigData := []byte("apiVersion: v1\nkind: Config\nclusters: []")
			// Use fixed condition timestamps so UpdateTime assertion is deterministic
			availableTime := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
			progressingTime := time.Date(2026, 3, 15, 9, 30, 0, 0, time.UTC)
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(
					metav1.Condition{Type: "Available", Status: metav1.ConditionTrue, LastTransitionTime: metav1.NewTime(availableTime)},
					metav1.Condition{Type: "Progressing", Status: metav1.ConditionFalse, LastTransitionTime: metav1.NewTime(progressingTime)},
				),
				withAPIEndpoint("api.cluster.example.com", 6443),
				withKubeConfigRef("my-cluster-admin-kubeconfig"),
				withBaseDomain("example.com"),
			)
			secret := buildKubeconfigSecret("my-cluster-admin-kubeconfig", testNamespace, kubeconfigData)
			svc, _ := newTestService(cfg, hc, secret)

			result, err := svc.Get(ctx, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.ClusterStatusREADY))
			Expect(result.ApiEndpoint).NotTo(BeNil())
			Expect(*result.ApiEndpoint).To(Equal("https://api.cluster.example.com:6443"))
			Expect(result.ConsoleUri).NotTo(BeNil())
			Expect(*result.ConsoleUri).To(Equal("https://console-openshift-console.apps.my-cluster.example.com"))
			Expect(result.Kubeconfig).NotTo(BeNil())
			Expect(*result.Kubeconfig).To(Equal(base64.StdEncoding.EncodeToString(kubeconfigData)))

			// MF-1: Version must be K8s minor version, not release image URL
			// Fixture release image is 4.17.0 → K8s 1.30 per compatibility matrix
			Expect(result.Spec.Version).To(Equal("1.30"))

			// MF-2: UpdateTime must be the latest condition LastTransitionTime
			Expect(result.UpdateTime).NotTo(BeNil())
			Expect(*result.UpdateTime).To(BeTemporally("==", availableTime))
		})

		It("TC-KV-UT-011: PROVISIONING cluster has empty credentials", func() {
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(provisioningConditions()...),
			)
			svc, _ := newTestService(cfg, hc)

			result, err := svc.Get(ctx, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.ApiEndpoint).To(BeNil())
			Expect(result.ConsoleUri).To(BeNil())
			Expect(result.Kubeconfig).To(BeNil())
		})

		It("TC-KV-UT-012: not found returns NotFound error", func() {
			svc, _ := newTestService(cfg)

			_, err := svc.Get(ctx, "nonexistent")

			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-018: kubeconfig Secret missing for READY cluster", func() {
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(readyConditions()...),
				withAPIEndpoint("api.cluster.example.com", 6443),
				withKubeConfigRef("my-cluster-admin-kubeconfig"),
			)
			// No secret created
			svc, _ := newTestService(cfg, hc)

			result, err := svc.Get(ctx, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.ClusterStatusREADY))
			Expect(result.ApiEndpoint).NotTo(BeNil())
			// Kubeconfig should be empty (graceful degradation)
			Expect(result.Kubeconfig).To(SatisfyAny(BeNil(), HaveValue(BeEmpty())))
		})

		It("TC-KV-UT-019: console URI constructed from pattern", func() {
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(readyConditions()...),
				withBaseDomain("example.com"),
			)
			svc, _ := newTestService(cfg, hc)

			result, err := svc.Get(ctx, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.ClusterStatusREADY))
			Expect(result.ConsoleUri).NotTo(BeNil())
			Expect(*result.ConsoleUri).To(Equal("https://console-openshift-console.apps.my-cluster.example.com"))
		})

		It("TC-KV-UT-021: K8s Get() transient error returns internal error without leaking", func() {
			// Stub returns nil, nil — test fails because we expect an error
			svc, _ := newTestService(cfg)

			_, err := svc.Get(ctx, "test-id")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-024: duplicate dcm-instance-id returns deterministic result", func() {
			hc1 := buildHostedCluster("cluster-a", testNamespace, withDCMLabels("test-id"))
			hc2 := buildHostedCluster("cluster-b", testNamespace, withDCMLabels("test-id"))
			svc, _ := newTestService(cfg, hc1, hc2)

			result, err := svc.Get(ctx, "test-id")

			// Either returns a deterministic result or an error
			if err == nil {
				Expect(result).NotTo(BeNil())
			}
		})

		It("TC-KV-UT-025: kubeconfig Secret exists but missing kubeconfig key", func() {
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(readyConditions()...),
				withAPIEndpoint("api.cluster.example.com", 6443),
				withKubeConfigRef("my-cluster-admin-kubeconfig"),
			)
			// Secret exists but with wrong key
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster-admin-kubeconfig",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"wrong-key": []byte("data"),
				},
			}
			svc, _ := newTestService(cfg, hc, secret)

			result, err := svc.Get(ctx, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.ClusterStatusREADY))
			Expect(result.Kubeconfig).To(SatisfyAny(BeNil(), HaveValue(BeEmpty())))
		})

		It("TC-KV-UT-027: UNAVAILABLE cluster still includes credentials", func() {
			kubeconfigData := []byte("apiVersion: v1\nkind: Config\nclusters: []")
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(unavailableConditions()...),
				withAPIEndpoint("api.cluster.example.com", 6443),
				withKubeConfigRef("my-cluster-admin-kubeconfig"),
				withBaseDomain("example.com"),
			)
			secret := buildKubeconfigSecret("my-cluster-admin-kubeconfig", testNamespace, kubeconfigData)
			svc, _ := newTestService(cfg, hc, secret)

			result, err := svc.Get(ctx, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.ClusterStatusUNAVAILABLE))
			Expect(result.ApiEndpoint).NotTo(BeNil())
			Expect(*result.ApiEndpoint).To(Equal("https://api.cluster.example.com:6443"))
			Expect(result.ConsoleUri).NotTo(BeNil())
			Expect(*result.ConsoleUri).To(Equal("https://console-openshift-console.apps.my-cluster.example.com"))
			Expect(result.Kubeconfig).NotTo(BeNil())
			Expect(*result.Kubeconfig).To(Equal(base64.StdEncoding.EncodeToString(kubeconfigData)))
		})
	})

	// ── List ────────────────────────────────────────────────────────────

	Describe("List", func() {
		It("TC-KV-UT-023: K8s List() error returns internal error", func() {
			svc, _ := newTestServiceWithInterceptor(cfg, nil, interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return fmt.Errorf("simulated list failure")
				},
			})

			_, err := svc.List(ctx, 50, "")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-030: invalid page_token returns InvalidArgument error", func() {
			hc := buildHostedCluster("my-cluster", testNamespace, withDCMLabels("test-id"))
			svc, _ := newTestService(cfg, hc)

			_, err := svc.List(ctx, 50, "!!!invalid!!!")

			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(errors.As(err, &domainErr)).To(BeTrue())
			Expect(domainErr.Type).To(Equal(v1alpha1.ErrorTypeINVALIDARGUMENT))
		})

		It("TC-KV-UT-026: results ordered by metadata.name ascending", func() {
			hc1 := buildHostedCluster("charlie", testNamespace, withDCMLabels("id-c"))
			hc2 := buildHostedCluster("alpha", testNamespace, withDCMLabels("id-a"))
			hc3 := buildHostedCluster("bravo", testNamespace, withDCMLabels("id-b"))
			svc, _ := newTestService(cfg, hc1, hc2, hc3)

			result, err := svc.List(ctx, 50, "")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Clusters).NotTo(BeNil())
			clusters := *result.Clusters
			Expect(clusters).To(HaveLen(3))
			Expect(clusters[0].Spec.Metadata.Name).To(Equal("alpha"))
			Expect(clusters[1].Spec.Metadata.Name).To(Equal("bravo"))
			Expect(clusters[2].Spec.Metadata.Name).To(Equal("charlie"))
		})
	})

	// ── Delete ──────────────────────────────────────────────────────────

	Describe("Delete", func() {
		It("TC-KV-UT-013: deletes HostedCluster and associated NodePools", func() {
			hc := buildHostedCluster("my-cluster", testNamespace, withDCMLabels("test-id"))
			np := buildNodePool("my-cluster", testNamespace, withNPDCMLabels("test-id"))
			svc, k8s := newTestService(cfg, hc, np)

			err := svc.Delete(ctx, "test-id")

			Expect(err).NotTo(HaveOccurred())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(BeEmpty())

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(BeEmpty())
		})

		It("TC-KV-UT-022: K8s Delete() error returns internal error", func() {
			// Stub returns nil — test fails because we expect an error
			svc, _ := newTestService(cfg)

			err := svc.Delete(ctx, "nonexistent")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-031: NodePool list failure during delete returns internal error", func() {
			hc := buildHostedCluster("my-cluster", testNamespace, withDCMLabels("test-id"))
			svc, _ := newTestServiceWithInterceptor(cfg, []client.Object{hc}, interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*hyperv1.NodePoolList); ok {
						return fmt.Errorf("simulated NP list failure")
					}
					return c.List(ctx, list, opts...)
				},
			})

			err := svc.Delete(ctx, "test-id")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(errors.As(err, &domainErr)).To(BeTrue())
		})
	})
})
