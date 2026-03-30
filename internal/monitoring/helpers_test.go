package monitoring_test

import (
	"context"
	"sync"
	"time"

	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/monitoring"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

const (
	testNamespace    = "clusters"
	testProviderName = "acm-cluster-sp"
)

// ── Mock StatusPublisher ─────────────────────────────────────────────

// mockStatusPublisher captures published events for assertions.
type mockStatusPublisher struct {
	mu        sync.Mutex
	events    []monitoring.StatusEvent
	err       error
	errFn     func() error
	callCount int
}

var _ monitoring.StatusPublisher = (*mockStatusPublisher)(nil)

func newMockPublisher() *mockStatusPublisher {
	return &mockStatusPublisher{}
}

func (m *mockStatusPublisher) Publish(_ context.Context, event monitoring.StatusEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.errFn != nil {
		if err := m.errFn(); err != nil {
			return err
		}
	} else if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

func (m *mockStatusPublisher) Close() error { return nil }

func (m *mockStatusPublisher) Events() []monitoring.StatusEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]monitoring.StatusEvent, len(m.events))
	copy(result, m.events)
	return result
}

func (m *mockStatusPublisher) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func (m *mockStatusPublisher) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
	m.errFn = nil
}

func (m *mockStatusPublisher) SetErrorFunc(fn func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = nil
	m.errFn = fn
}

func (m *mockStatusPublisher) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
	m.callCount = 0
	m.err = nil
}

// ── HostedCluster GVR ────────────────────────────────────────────────

func hostedClusterGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "hypershift.openshift.io",
		Version:  "v1beta1",
		Resource: "hostedclusters",
	}
}

// ── Dynamic fake client ──────────────────────────────────────────────

func newDynamicFakeClient(objects ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	objs := make([]runtime.Object, len(objects))
	for i, obj := range objects {
		objs[i] = obj
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			hostedClusterGVR(): "HostedClusterList",
		},
		objs...,
	)
}

// ── Unstructured HostedCluster builder ───────────────────────────────

type hcUnstructuredOption func(*unstructured.Unstructured)

func buildUnstructuredHostedCluster(name, instanceID string, conditions []metav1.Condition, opts ...hcUnstructuredOption) *unstructured.Unstructured {
	labels := cluster.DCMLabels(instanceID)

	condList := make([]any, 0, len(conditions))
	for _, c := range conditions {
		condMap := map[string]any{
			"type":               c.Type,
			"status":             string(c.Status),
			"lastTransitionTime": c.LastTransitionTime.Format(time.RFC3339),
			"reason":             c.Reason,
			"message":            c.Message,
		}
		condList = append(condList, condMap)
	}

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "hypershift.openshift.io/v1beta1",
			"kind":       "HostedCluster",
			"metadata": map[string]any{
				"name":      name,
				"namespace": testNamespace,
				"labels":    toStringInterfaceMap(labels),
			},
			"status": map[string]any{
				"conditions": condList,
			},
		},
	}

	for _, opt := range opts {
		opt(obj)
	}
	return obj
}

func toStringInterfaceMap(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func withDeletionTimestamp(t time.Time) hcUnstructuredOption {
	return func(obj *unstructured.Unstructured) {
		ts := metav1.NewTime(t)
		obj.SetDeletionTimestamp(&ts)
	}
}

func withoutDCMLabels() hcUnstructuredOption {
	return func(obj *unstructured.Unstructured) {
		obj.SetLabels(map[string]string{})
	}
}

func withoutInstanceIDLabel() hcUnstructuredOption {
	return func(obj *unstructured.Unstructured) {
		labels := obj.GetLabels()
		delete(labels, cluster.LabelInstanceID)
		obj.SetLabels(labels)
	}
}

// ── Default MonitorConfig ────────────────────────────────────────────

func defaultMonitorConfig() monitoring.MonitorConfig {
	return monitoring.MonitorConfig{
		Namespace:            testNamespace,
		ProviderName:         testProviderName,
		DebounceInterval:     50 * time.Millisecond,
		ResyncInterval:       10 * time.Minute,
		PublishRetryMax:      3,
		PublishRetryInterval: 10 * time.Millisecond,
	}
}

// ── Condition presets ────────────────────────────────────────────────

func readyConditions() []metav1.Condition {
	return []metav1.Condition{
		{Type: "Available", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
		{Type: "Progressing", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
	}
}

func provisioningConditions() []metav1.Condition {
	return []metav1.Condition{
		{Type: "Progressing", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
		{Type: "Available", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
	}
}

func failedConditions() []metav1.Condition {
	return []metav1.Condition{
		{Type: "Degraded", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now(), Message: "etcd cluster is degraded"},
	}
}

func failedConditionsWithMessage(msg string) []metav1.Condition {
	return []metav1.Condition{
		{Type: "Degraded", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now(), Message: msg},
	}
}

func unavailableConditions() []metav1.Condition {
	return []metav1.Condition{
		{Type: "Available", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now(), Message: "Kube API server is not ready"},
		{Type: "Progressing", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
	}
}

func unavailableConditionsWithMessage(msg string) []metav1.Condition {
	return []metav1.Condition{
		{Type: "Available", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now(), Message: msg},
		{Type: "Progressing", Status: metav1.ConditionFalse, LastTransitionTime: metav1.Now()},
	}
}
