package handler

import (
	"encoding/json"
	"fmt"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
)

// toServiceCluster converts an oapigen.Cluster to a v1alpha1.Cluster via JSON roundtrip.
func toServiceCluster(c oapigen.Cluster) (v1alpha1.Cluster, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return v1alpha1.Cluster{}, fmt.Errorf("marshal oapigen cluster: %w", err)
	}
	var result v1alpha1.Cluster
	if err := json.Unmarshal(data, &result); err != nil {
		return v1alpha1.Cluster{}, fmt.Errorf("unmarshal v1alpha1 cluster: %w", err)
	}
	return result, nil
}

// toAPICluster converts a v1alpha1.Cluster to an oapigen.Cluster via JSON roundtrip.
func toAPICluster(c *v1alpha1.Cluster) (oapigen.Cluster, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return oapigen.Cluster{}, fmt.Errorf("marshal v1alpha1 cluster: %w", err)
	}
	var result oapigen.Cluster
	if err := json.Unmarshal(data, &result); err != nil {
		return oapigen.Cluster{}, fmt.Errorf("unmarshal oapigen cluster: %w", err)
	}
	return result, nil
}

// toAPIClusterList converts a v1alpha1.ClusterList to an oapigen.ClusterList via JSON roundtrip.
func toAPIClusterList(c *v1alpha1.ClusterList) (oapigen.ClusterList, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return oapigen.ClusterList{}, fmt.Errorf("marshal v1alpha1 cluster list: %w", err)
	}
	var result oapigen.ClusterList
	if err := json.Unmarshal(data, &result); err != nil {
		return oapigen.ClusterList{}, fmt.Errorf("unmarshal oapigen cluster list: %w", err)
	}
	return result, nil
}
