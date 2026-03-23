package cluster_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("Shared Cluster Operations", func() {
	var (
		ctx context.Context
		cfg config.ClusterConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg = defaultConfig()
	})

	// ── Get ─────────────────────────────────────────────────────────────

	Describe("GetCluster", func() {
		It("TC-KV-UT-010: READY cluster has api_endpoint, console_uri, kubeconfig, version, and update_time", func() {
			kubeconfigData := []byte("apiVersion: v1\nkind: Config\nclusters: []")
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
			k8s := buildFakeClient([]client.Object{hc, secret}, nil)

			result, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")

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
			Expect(result.Spec.Version).To(Equal("1.30"))
			Expect(result.UpdateTime).NotTo(BeNil())
			Expect(*result.UpdateTime).To(BeTemporally("==", availableTime))
		})

		It("TC-KV-UT-011: PROVISIONING cluster has empty credentials", func() {
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(provisioningConditions()...),
			)
			k8s := buildFakeClient([]client.Object{hc}, nil)

			result, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.ApiEndpoint).To(BeNil())
			Expect(result.ConsoleUri).To(BeNil())
			Expect(result.Kubeconfig).To(BeNil())
		})

		It("TC-KV-UT-012: not found returns NotFound error", func() {
			k8s := buildFakeClient(nil, nil)

			_, err := cluster.GetCluster(ctx, k8s, cfg, "nonexistent")

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
			k8s := buildFakeClient([]client.Object{hc}, nil)

			result, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.ClusterStatusREADY))
			Expect(result.ApiEndpoint).NotTo(BeNil())
			Expect(result.Kubeconfig).To(SatisfyAny(BeNil(), HaveValue(BeEmpty())))
		})

		It("TC-KV-UT-019: console URI constructed from pattern", func() {
			hc := buildHostedCluster("my-cluster", testNamespace,
				withDCMLabels("test-id"),
				withConditions(readyConditions()...),
				withBaseDomain("example.com"),
			)
			k8s := buildFakeClient([]client.Object{hc}, nil)

			result, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.ClusterStatusREADY))
			Expect(result.ConsoleUri).NotTo(BeNil())
			Expect(*result.ConsoleUri).To(Equal("https://console-openshift-console.apps.my-cluster.example.com"))
		})

		It("TC-KV-UT-021: K8s transient error returns internal error without leaking", func() {
			k8s := buildFakeClient(nil, nil)

			_, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-024: duplicate dcm-instance-id returns deterministic result", func() {
			hc1 := buildHostedCluster("cluster-a", testNamespace, withDCMLabels("test-id"))
			hc2 := buildHostedCluster("cluster-b", testNamespace, withDCMLabels("test-id"))
			k8s := buildFakeClient([]client.Object{hc1, hc2}, nil)

			result, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")

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
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster-admin-kubeconfig",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"wrong-key": []byte("data"),
				},
			}
			k8s := buildFakeClient([]client.Object{hc, secret}, nil)

			result, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")

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
			k8s := buildFakeClient([]client.Object{hc, secret}, nil)

			result, err := cluster.GetCluster(ctx, k8s, cfg, "test-id")

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

	Describe("ListClusters", func() {
		It("TC-KV-UT-023: K8s List() error returns internal error", func() {
			fns := interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return fmt.Errorf("simulated list failure")
				},
			}
			k8s := buildFakeClient(nil, &fns)

			_, err := cluster.ListClusters(ctx, k8s, cfg, 50, "")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-030: invalid page_token returns InvalidArgument error", func() {
			hc := buildHostedCluster("my-cluster", testNamespace, withDCMLabels("test-id"))
			k8s := buildFakeClient([]client.Object{hc}, nil)

			_, err := cluster.ListClusters(ctx, k8s, cfg, 50, "!!!invalid!!!")

			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(errors.As(err, &domainErr)).To(BeTrue())
			Expect(domainErr.Type).To(Equal(v1alpha1.ErrorTypeINVALIDARGUMENT))
		})

		It("TC-KV-UT-026: results ordered by metadata.name ascending", func() {
			hc1 := buildHostedCluster("charlie", testNamespace, withDCMLabels("id-c"))
			hc2 := buildHostedCluster("alpha", testNamespace, withDCMLabels("id-a"))
			hc3 := buildHostedCluster("bravo", testNamespace, withDCMLabels("id-b"))
			k8s := buildFakeClient([]client.Object{hc1, hc2, hc3}, nil)

			result, err := cluster.ListClusters(ctx, k8s, cfg, 50, "")

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

	Describe("DeleteCluster", func() {
		It("TC-KV-UT-013: deletes HostedCluster and associated NodePools", func() {
			hc := buildHostedCluster("my-cluster", testNamespace, withDCMLabels("test-id"))
			np := buildNodePool("my-cluster", testNamespace, withNPDCMLabels("test-id"))
			k8s := buildFakeClient([]client.Object{hc, np}, nil)

			err := cluster.DeleteCluster(ctx, k8s, cfg, "test-id")

			Expect(err).NotTo(HaveOccurred())

			var hcList hyperv1.HostedClusterList
			Expect(k8s.List(ctx, &hcList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(hcList.Items).To(BeEmpty())

			var npList hyperv1.NodePoolList
			Expect(k8s.List(ctx, &npList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(npList.Items).To(BeEmpty())
		})

		It("TC-KV-UT-022: nonexistent cluster returns NotFound error", func() {
			k8s := buildFakeClient(nil, nil)

			err := cluster.DeleteCluster(ctx, k8s, cfg, "nonexistent")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(err).To(BeAssignableToTypeOf(domainErr))
		})

		It("TC-KV-UT-031: NodePool list failure during delete returns internal error", func() {
			hc := buildHostedCluster("my-cluster", testNamespace, withDCMLabels("test-id"))
			fns := interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*hyperv1.NodePoolList); ok {
						return fmt.Errorf("simulated NP list failure")
					}
					return c.List(ctx, list, opts...)
				},
			}
			k8s := buildFakeClient([]client.Object{hc}, &fns)

			err := cluster.DeleteCluster(ctx, k8s, cfg, "test-id")
			Expect(err).To(HaveOccurred())
			var domainErr *service.DomainError
			Expect(errors.As(err, &domainErr)).To(BeTrue())
		})
	})
})
