package kubevirtprovider_test

import (
	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster/kubevirtprovider"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testNamespace  = "test-clusters"
	testBaseDomain = "example.com"
)

// defaultConfig returns a cluster.Config with sensible test defaults.
func defaultConfig() cluster.Config {
	return cluster.Config{
		ClusterNamespace:  testNamespace,
		BaseDomain:        testBaseDomain,
		ConsoleURIPattern: "https://console-openshift-console.apps.{name}.{base_domain}",
	}
}

// buildFakeClient creates a fake K8s client with the scheme, REST mapper for
// ClusterImageSets, and default CIS objects pre-seeded.
func buildFakeClient(objs []client.Object, fns *interceptor.Funcs) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = hyperv1.AddToScheme(scheme)

	// Register ClusterImageSet GVK in REST mapper for unstructured support
	restMapper := meta.NewDefaultRESTMapper(nil)
	restMapper.Add(schema.GroupVersionKind{
		Group:   "hypershift.openshift.io",
		Version: "v1beta1",
		Kind:    "ClusterImageSet",
	}, meta.RESTScopeRoot)

	allObjs := append(defaultClusterImageSets(), objs...)

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRESTMapper(restMapper).
		WithObjects(allObjs...)

	if fns != nil {
		builder = builder.WithInterceptorFuncs(*fns)
	}

	return builder.Build()
}

// newTestService creates a KubeVirt Service backed by a fake K8s client.
func newTestService(cfg cluster.Config, objs ...client.Object) (*kubevirtprovider.Service, client.Client) {
	c := buildFakeClient(objs, nil)
	return kubevirtprovider.New(c, cfg), c
}

// newTestServiceWithInterceptor creates a KubeVirt Service with a fake K8s client
// that uses interceptor functions to simulate failures.
func newTestServiceWithInterceptor(cfg cluster.Config, objs []client.Object, fns interceptor.Funcs) (*kubevirtprovider.Service, client.Client) { //nolint:unparam // objs kept for flexibility
	c := buildFakeClient(objs, &fns)
	return kubevirtprovider.New(c, cfg), c
}

// defaultClusterImageSets returns CIS objects for OCP 4.15 and 4.17,
// matching compatibility matrix entries for K8s 1.28 and 1.30.
func defaultClusterImageSets() []client.Object {
	return []client.Object{
		buildClusterImageSet("ocp-4.15.2", "quay.io/openshift-release-dev/ocp-release:4.15.2-x86_64"),
		buildClusterImageSet("ocp-4.17.0", "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64"),
	}
}

// buildClusterImageSet creates an unstructured ClusterImageSet object.
func buildClusterImageSet(name, releaseImage string) *unstructured.Unstructured {
	cis := &unstructured.Unstructured{}
	cis.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "hypershift.openshift.io",
		Version: "v1beta1",
		Kind:    "ClusterImageSet",
	})
	cis.SetName(name)
	_ = unstructured.SetNestedField(cis.Object, releaseImage, "spec", "releaseImage")
	return cis
}

// npOption is a functional option for building NodePool test fixtures.
type npOption func(*hyperv1.NodePool)

func withNPDCMLabels(instanceID string) npOption {
	return func(np *hyperv1.NodePool) {
		if np.Labels == nil {
			np.Labels = make(map[string]string)
		}
		np.Labels["app.kubernetes.io/managed-by"] = "dcm"
		np.Labels["dcm-instance-id"] = instanceID
		np.Labels["dcm-service-type"] = "cluster"
	}
}

// buildNodePool creates a typed NodePool fixture with functional options.
func buildNodePool(name, namespace string, opts ...npOption) *hyperv1.NodePool {
	np := &hyperv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: hyperv1.NodePoolSpec{
			ClusterName: name,
			Platform: hyperv1.NodePoolPlatform{
				Type: hyperv1.KubevirtPlatform,
			},
		},
	}
	for _, opt := range opts {
		opt(np)
	}
	return np
}

// hcOption is a functional option for building HostedCluster test fixtures.
type hcOption func(*hyperv1.HostedCluster)

func withConditions(conditions ...metav1.Condition) hcOption {
	return func(hc *hyperv1.HostedCluster) {
		hc.Status.Conditions = conditions
	}
}

func withKubeConfigRef(secretName string) hcOption { //nolint:unparam // kept for flexibility
	return func(hc *hyperv1.HostedCluster) {
		hc.Status.KubeConfig = &corev1.LocalObjectReference{Name: secretName}
	}
}

func withAPIEndpoint(host string, port int32) hcOption { //nolint:unparam // kept for flexibility
	return func(hc *hyperv1.HostedCluster) {
		hc.Status.ControlPlaneEndpoint = hyperv1.APIEndpoint{Host: host, Port: port}
	}
}

func withDCMLabels(instanceID string) hcOption {
	return func(hc *hyperv1.HostedCluster) {
		if hc.Labels == nil {
			hc.Labels = make(map[string]string)
		}
		hc.Labels["app.kubernetes.io/managed-by"] = "dcm"
		hc.Labels["dcm-instance-id"] = instanceID
		hc.Labels["dcm-service-type"] = "cluster"
	}
}

func withBaseDomain(domain string) hcOption {
	return func(hc *hyperv1.HostedCluster) {
		hc.Spec.DNS.BaseDomain = domain
	}
}

// buildHostedCluster creates a typed HostedCluster fixture with functional options.
func buildHostedCluster(name, namespace string, opts ...hcOption) *hyperv1.HostedCluster { //nolint:unparam // namespace kept for flexibility
	hc := &hyperv1.HostedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: hyperv1.HostedClusterSpec{
			Platform: hyperv1.PlatformSpec{
				Type: hyperv1.KubevirtPlatform,
			},
			Release: hyperv1.Release{
				Image: "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64",
			},
		},
	}
	for _, opt := range opts {
		opt(hc)
	}
	return hc
}

// buildKubeconfigSecret creates a Secret containing kubeconfig data.
func buildKubeconfigSecret(name, namespace string, kubeconfigData []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"kubeconfig": kubeconfigData,
		},
	}
}

// validCreateCluster returns a v1alpha1.Cluster suitable for Create() calls.
func validCreateCluster() v1alpha1.Cluster {
	return v1alpha1.Cluster{
		Spec: v1alpha1.ClusterSpec{
			Version:     "1.30",
			ServiceType: v1alpha1.ClusterSpecServiceTypeCluster,
			Metadata: v1alpha1.ClusterMetadata{
				Name: "test-cluster",
			},
			Nodes: v1alpha1.ClusterNodes{
				ControlPlane: v1alpha1.ControlPlaneSpec{
					Count:   v1alpha1.N3,
					Cpu:     4,
					Memory:  "16GB",
					Storage: "120GB",
				},
				Workers: v1alpha1.WorkerSpec{
					Count:   3,
					Cpu:     8,
					Memory:  "32GB",
					Storage: "500GB",
				},
			},
		},
	}
}

// readyConditions returns HyperShift conditions indicating a READY cluster.
func readyConditions() []metav1.Condition {
	return []metav1.Condition{
		{Type: "Available", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
		{Type: "Progressing", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
	}
}

// provisioningConditions returns HyperShift conditions indicating PROVISIONING.
func provisioningConditions() []metav1.Condition {
	return []metav1.Condition{
		{Type: "Progressing", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
		{Type: "Available", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
	}
}

// unavailableConditions returns HyperShift conditions indicating UNAVAILABLE.
func unavailableConditions() []metav1.Condition {
	return []metav1.Condition{
		{Type: "Available", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
		{Type: "Progressing", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
	}
}
