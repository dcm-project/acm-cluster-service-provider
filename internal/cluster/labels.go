package cluster

const (
	LabelManagedBy   = "app.kubernetes.io/managed-by"
	LabelInstanceID  = "dcm-instance-id"
	LabelServiceType = "dcm-service-type"
	ValueManagedBy   = "dcm"
	ValueServiceType = "cluster"
)

// DCMLabels returns the standard set of DCM labels for a cluster resource.
func DCMLabels(instanceID string) map[string]string {
	return map[string]string{
		LabelManagedBy:   ValueManagedBy,
		LabelInstanceID:  instanceID,
		LabelServiceType: ValueServiceType,
	}
}
