package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var hostedClusterGVR = schema.GroupVersionResource{
	Group:    "hypershift.openshift.io",
	Version:  "v1beta1",
	Resource: "hostedclusters",
}

// StatusMonitor watches HostedCluster resources and publishes status CloudEvents.
type StatusMonitor struct {
	dynamicClient dynamic.Interface
	cfg           MonitorConfig
	publisher     StatusPublisher
	logger        *slog.Logger
}

// New creates a new StatusMonitor.
func New(dynamicClient dynamic.Interface, cfg MonitorConfig, publisher StatusPublisher, logger *slog.Logger) *StatusMonitor {
	return &StatusMonitor{
		dynamicClient: dynamicClient,
		cfg:           cfg,
		publisher:     publisher,
		logger:        logger,
	}
}

// Start begins watching HostedCluster resources. It blocks until ctx is cancelled.
func (m *StatusMonitor) Start(ctx context.Context) error {
	lastStatus := make(map[string]v1alpha1.ClusterStatus)
	seenInstanceIDs := make(map[string]string)
	var mu sync.Mutex

	selector := fmt.Sprintf("%s=%s,%s=%s",
		cluster.LabelManagedBy, cluster.ValueManagedBy,
		cluster.LabelServiceType, cluster.ValueServiceType,
	)

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		m.dynamicClient,
		m.cfg.ResyncInterval,
		m.cfg.Namespace,
		func(opts *metav1.ListOptions) {
			opts.LabelSelector = selector
		},
	)

	informer := factory.ForResource(hostedClusterGVR).Informer()

	if err := informer.AddIndexers(cache.Indexers{
		InstanceIDIndex: InstanceIDIndexFunc,
	}); err != nil {
		return fmt.Errorf("adding indexers: %w", err)
	}

	debouncer := NewDebouncer(m.cfg.DebounceInterval, func(event StatusEvent) {
		m.publishWithRetry(ctx, event)
	})

	reconcile := func(obj any) {
		uns, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
		event := m.reconcileObject(uns, lastStatus, seenInstanceIDs, &mu)
		if event != nil {
			debouncer.Submit(uns.GetName(), *event)
		}
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: reconcile,
		UpdateFunc: func(_, newObj any) {
			reconcile(newObj)
		},
		DeleteFunc: func(obj any) {
			name, event := m.handleDeleteObject(obj, lastStatus, seenInstanceIDs, &mu)
			if event != nil {
				debouncer.Submit(name, *event)
			}
		},
	}); err != nil {
		return fmt.Errorf("adding event handler: %w", err)
	}

	factory.Start(ctx.Done())
	defer factory.Shutdown()
	defer debouncer.Stop()

	synced := factory.WaitForCacheSync(ctx.Done())
	if ctx.Err() == nil {
		for gvr, ok := range synced {
			if !ok {
				m.logger.Error("cache sync failed, aborting start", "type", gvr)
				return fmt.Errorf("cache sync failed for %v", gvr)
			}
		}
	}

	<-ctx.Done()
	return nil
}

// reconcileObject maps an unstructured HostedCluster to a StatusEvent, applying dedup.
func (m *StatusMonitor) reconcileObject(obj *unstructured.Unstructured, lastStatus map[string]v1alpha1.ClusterStatus, seenInstanceIDs map[string]string, mu *sync.Mutex) *StatusEvent {
	instanceID := extractInstanceID(obj)
	if instanceID == "" {
		m.logger.Warn("skipping resource with missing dcm-instance-id",
			"name", obj.GetName(), "namespace", obj.GetNamespace())
		return nil
	}

	conditions := extractConditions(obj)
	deletionTimestamp := obj.GetDeletionTimestamp()
	dcmStatus := status.MapConditionsToStatus(conditions, deletionTimestamp)

	mu.Lock()
	if existing, ok := seenInstanceIDs[instanceID]; ok && existing != obj.GetName() {
		m.logger.Warn("duplicate dcm-instance-id detected",
			"instanceID", instanceID,
			"resource", obj.GetName(),
			"existingResource", existing,
		)
	} else if !ok {
		seenInstanceIDs[instanceID] = obj.GetName()
	}
	if lastStatus[obj.GetName()] == dcmStatus {
		mu.Unlock()
		return nil
	}
	lastStatus[obj.GetName()] = dcmStatus
	mu.Unlock()

	msg := extractMessage(conditions, dcmStatus)
	return &StatusEvent{
		InstanceID: instanceID,
		Status:     dcmStatus,
		Message:    msg,
	}
}

// handleDeleteObject processes a delete event and returns the resource name and event.
func (m *StatusMonitor) handleDeleteObject(obj any, lastStatus map[string]v1alpha1.ClusterStatus, seenInstanceIDs map[string]string, mu *sync.Mutex) (string, *StatusEvent) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	uns, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return "", nil
	}
	id := extractInstanceID(uns)
	if id == "" {
		m.logger.Warn("skipping resource with missing dcm-instance-id",
			"name", uns.GetName(), "namespace", uns.GetNamespace())
		return "", nil
	}

	mu.Lock()
	delete(seenInstanceIDs, id)
	lastStatus[uns.GetName()] = v1alpha1.ClusterStatusDELETED
	mu.Unlock()

	return uns.GetName(), &StatusEvent{
		InstanceID: id,
		Status:     v1alpha1.ClusterStatusDELETED,
	}
}

// publishWithRetry publishes an event with exponential backoff retry.
func (m *StatusMonitor) publishWithRetry(ctx context.Context, event StatusEvent) {
	backoff := m.cfg.PublishRetryInterval
	for attempt := 0; attempt <= m.cfg.PublishRetryMax; attempt++ {
		if err := m.publisher.Publish(ctx, event); err != nil {
			m.logger.Warn("publish failed",
				"instanceID", event.InstanceID,
				"attempt", attempt+1,
				"error", err,
			)
			if attempt < m.cfg.PublishRetryMax {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff *= 2
			}
			continue
		}
		return
	}
	m.logger.Warn("dropping event after retry exhaustion",
		"instanceID", event.InstanceID,
	)
}

// extractConditions parses status.conditions from an unstructured HostedCluster.
func extractConditions(obj *unstructured.Unstructured) []metav1.Condition {
	statusObj, ok := obj.Object["status"].(map[string]any)
	if !ok {
		return nil
	}
	condSlice, ok := statusObj["conditions"].([]any)
	if !ok {
		return nil
	}

	conditions := make([]metav1.Condition, 0, len(condSlice))
	for _, item := range condSlice {
		condMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		c := metav1.Condition{}
		if t, ok := condMap["type"].(string); ok {
			c.Type = t
		}
		if s, ok := condMap["status"].(string); ok {
			c.Status = metav1.ConditionStatus(s)
		}
		if msg, ok := condMap["message"].(string); ok {
			c.Message = msg
		}
		conditions = append(conditions, c)
	}
	return conditions
}

// extractMessage returns a human-readable message for FAILED or UNAVAILABLE statuses.
func extractMessage(conditions []metav1.Condition, dcmStatus v1alpha1.ClusterStatus) string {
	switch dcmStatus {
	case v1alpha1.ClusterStatusFAILED:
		for i := range conditions {
			if conditions[i].Type == "Degraded" {
				return conditions[i].Message
			}
		}
	case v1alpha1.ClusterStatusUNAVAILABLE:
		for i := range conditions {
			if conditions[i].Type == "Available" {
				return conditions[i].Message
			}
		}
	}
	return ""
}
