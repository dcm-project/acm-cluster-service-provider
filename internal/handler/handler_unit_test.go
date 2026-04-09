package handler_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/handler"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
)

// ---------------------------------------------------------------------------
// CreateCluster Handler Tests (17 TCs)
// ---------------------------------------------------------------------------

var _ = Describe("CreateCluster Handler", func() {
	var (
		h    *handler.Handler
		mock *mockClusterService
		ctx  context.Context
	)

	BeforeEach(func() {
		mock = &mockClusterService{}
		h = handler.New(mock, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		ctx = context.Background()
	})

	It("creates cluster with server-generated ID (TC-HDL-CRT-UT-001)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, id string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return clusterResult(id), nil
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		created, ok := resp.(oapigen.CreateCluster201JSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster201JSONResponse")
		Expect(created.Id).NotTo(BeNil())
		Expect(*created.Id).To(MatchRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`))
		Expect(created.Status).NotTo(BeNil())
		Expect(*created.Status).To(Equal(oapigen.ClusterStatusPENDING))
		Expect(created.Path).NotTo(BeNil())
	})

	It("creates cluster with client-specified ID (TC-HDL-CRT-UT-002)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Params: oapigen.CreateClusterParams{
				Id: util.Ptr("my-custom-id"),
			},
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, id string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return clusterResult(id), nil
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		created, ok := resp.(oapigen.CreateCluster201JSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster201JSONResponse")
		Expect(created.Id).NotTo(BeNil())
		Expect(*created.Id).To(Equal("my-custom-id"))
		Expect(created.Path).NotTo(BeNil())
		Expect(*created.Path).To(Equal("/api/v1alpha1/clusters/my-custom-id"))
	})

	It("returns 400 for invalid service_type (TC-HDL-CRT-UT-004)", func() {
		body := validClusterBody()
		body.Spec.ServiceType = "invalid"

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 400 when workers are missing (TC-HDL-CRT-UT-005)", func() {
		body := validClusterBody()
		body.Spec.Nodes.Workers = oapigen.WorkerSpec{}

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 400 for invalid memory format (TC-HDL-CRT-UT-006)", func() {
		body := validClusterBody()
		body.Spec.Nodes.Workers.Memory = "invalid"

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 409 for duplicate ID from service (TC-HDL-CRT-UT-007)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Params: oapigen.CreateClusterParams{
				Id: util.Ptr("existing-id"),
			},
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, _ string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return nil, service.NewAlreadyExistsError("cluster with id already exists")
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster409ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster409ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeALREADYEXISTS))
	})

	It("returns 409 for duplicate metadata.name from service (TC-HDL-CRT-UT-008)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, _ string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return nil, service.NewAlreadyExistsError("cluster with name already exists").
				WithDetail("cluster with name 'test-cluster' already exists")
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster409ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster409ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeALREADYEXISTS))
		Expect(errResp.Detail).NotTo(BeNil())
		Expect(*errResp.Detail).To(ContainSubstring("test-cluster"))
	})

	It("returns 422 for unsupported platform from service (TC-HDL-CRT-UT-009)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, _ string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return nil, service.NewUnprocessableEntityError("unsupported platform")
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster422ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster422ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeUNPROCESSABLEENTITY))
	})

	It("returns 422 for version not found from service (TC-HDL-CRT-UT-010)", func() {
		body := validClusterBody()
		body.Spec.Version = "9.99"

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, _ string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return nil, service.NewUnprocessableEntityError("version not found")
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster422ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster422ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeUNPROCESSABLEENTITY))
	})

	It("returns 400 for missing required fields (TC-HDL-CRT-UT-011)", func() {
		body := validClusterBody()
		body.Spec.Version = ""

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 500 for internal error without leaking details (TC-HDL-CRT-UT-012)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, _ string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return nil, service.NewInternalError("cluster creation failed",
				fmt.Errorf("k8s api error: connection refused to kube-apiserver:6443"))
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster500ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster500ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINTERNAL))
		if errResp.Detail != nil {
			Expect(*errResp.Detail).NotTo(ContainSubstring("k8s"))
			Expect(*errResp.Detail).NotTo(ContainSubstring("kube-apiserver"))
		}
	})

	It("returns 400 when nodes are missing entirely (TC-HDL-CRT-UT-013)", func() {
		body := validClusterBody()
		body.Spec.Nodes = oapigen.ClusterNodes{}

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 400 when workers count is below minimum (TC-HDL-CRT-UT-014)", func() {
		body := validClusterBody()
		body.Spec.Nodes.Workers.Count = 0

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 400 for invalid ?id= format (TC-HDL-CRT-UT-015)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Params: oapigen.CreateClusterParams{
				Id: util.Ptr("INVALID-UPPERCASE-ID!!!"),
			},
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 400 when service_type is missing (TC-HDL-CRT-UT-016)", func() {
		body := validClusterBody()
		body.Spec.ServiceType = ""

		req := oapigen.CreateClusterRequestObject{
			Body: &body,
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.CreateCluster400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("treats empty ?id= as absent and generates UUID (TC-HDL-CRT-UT-019)", func() {
		body := validClusterBody()
		req := oapigen.CreateClusterRequestObject{
			Params: oapigen.CreateClusterParams{
				Id: util.Ptr(""),
			},
			Body: &body,
		}

		mock.CreateFunc = func(_ context.Context, id string, _ v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
			return clusterResult(id), nil
		}

		resp, err := h.CreateCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		created, ok := resp.(oapigen.CreateCluster201JSONResponse)
		Expect(ok).To(BeTrue(), "expected CreateCluster201JSONResponse")
		Expect(created.Id).NotTo(BeNil())
		Expect(*created.Id).To(MatchRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`))
	})
})

// ---------------------------------------------------------------------------
// GetCluster Handler Tests (4 TCs)
// ---------------------------------------------------------------------------

var _ = Describe("GetCluster Handler", func() {
	var (
		h    *handler.Handler
		mock *mockClusterService
		ctx  context.Context
	)

	BeforeEach(func() {
		mock = &mockClusterService{}
		h = handler.New(mock, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		ctx = context.Background()
	})

	It("returns READY cluster with all fields populated (TC-HDL-GET-UT-001)", func() {
		mock.GetFunc = func(_ context.Context, id string) (*v1alpha1.Cluster, error) {
			return readyClusterResult(id), nil
		}

		req := oapigen.GetClusterRequestObject{ClusterId: "test-cluster-id"}

		resp, err := h.GetCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		cluster, ok := resp.(oapigen.GetCluster200JSONResponse)
		Expect(ok).To(BeTrue(), "expected GetCluster200JSONResponse")
		Expect(cluster.Id).NotTo(BeNil())
		Expect(*cluster.Id).To(Equal("test-cluster-id"))
		Expect(cluster.Status).NotTo(BeNil())
		Expect(*cluster.Status).To(Equal(oapigen.ClusterStatusREADY))
		Expect(cluster.ApiEndpoint).NotTo(BeNil())
		Expect(cluster.Kubeconfig).NotTo(BeNil())
		Expect(cluster.ConsoleUri).NotTo(BeNil())
	})

	It("returns non-READY cluster without credentials (TC-HDL-GET-UT-002)", func() {
		mock.GetFunc = func(_ context.Context, id string) (*v1alpha1.Cluster, error) {
			result := clusterResult(id)
			result.Status = util.Ptr(v1alpha1.ClusterStatusPENDING)
			return result, nil
		}

		req := oapigen.GetClusterRequestObject{ClusterId: "pending-cluster-id"}

		resp, err := h.GetCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		cluster, ok := resp.(oapigen.GetCluster200JSONResponse)
		Expect(ok).To(BeTrue(), "expected GetCluster200JSONResponse")
		Expect(cluster.Status).NotTo(BeNil())
		Expect(string(*cluster.Status)).To(Equal("PENDING"))
		Expect(cluster.ApiEndpoint).To(BeNil())
		Expect(cluster.Kubeconfig).To(BeNil())
		Expect(cluster.ConsoleUri).To(BeNil())
	})

	It("returns 404 for non-existent cluster (TC-HDL-GET-UT-003)", func() {
		mock.GetFunc = func(_ context.Context, _ string) (*v1alpha1.Cluster, error) {
			return nil, service.NewNotFoundError("cluster not found")
		}

		req := oapigen.GetClusterRequestObject{ClusterId: "non-existent-id"}

		resp, err := h.GetCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.GetCluster404ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected GetCluster404ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeNOTFOUND))
	})

	It("returns UNAVAILABLE cluster with credentials (TC-HDL-GET-UT-005)", func() {
		mock.GetFunc = func(_ context.Context, id string) (*v1alpha1.Cluster, error) {
			result := clusterResult(id)
			unavailable := v1alpha1.ClusterStatus("UNAVAILABLE")
			result.Status = &unavailable
			result.ApiEndpoint = util.Ptr("https://api.cluster.example.com:6443")
			result.Kubeconfig = util.Ptr("base64-kubeconfig-data")
			result.ConsoleUri = util.Ptr("https://console.cluster.example.com")
			return result, nil
		}

		req := oapigen.GetClusterRequestObject{ClusterId: "unavailable-cluster-id"}

		resp, err := h.GetCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		cluster, ok := resp.(oapigen.GetCluster200JSONResponse)
		Expect(ok).To(BeTrue(), "expected GetCluster200JSONResponse")
		Expect(cluster.Status).NotTo(BeNil())
		Expect(string(*cluster.Status)).To(Equal("UNAVAILABLE"))
		Expect(cluster.ApiEndpoint).NotTo(BeNil())
		Expect(cluster.Kubeconfig).NotTo(BeNil())
		Expect(cluster.ConsoleUri).NotTo(BeNil())
	})
})

// ---------------------------------------------------------------------------
// ListClusters Handler Tests (8 TCs)
// ---------------------------------------------------------------------------

var _ = Describe("ListClusters Handler", func() {
	var (
		h    *handler.Handler
		mock *mockClusterService
		ctx  context.Context
	)

	BeforeEach(func() {
		mock = &mockClusterService{}
		h = handler.New(mock, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		ctx = context.Background()
	})

	It("returns default pagination with 50 results (TC-HDL-LST-UT-001)", func() {
		mock.ListFunc = func(_ context.Context, pageSize int, _ string) (*v1alpha1.ClusterList, error) {
			return clusterListResult(pageSize, "next-token"), nil
		}

		req := oapigen.ListClustersRequestObject{}

		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		list, ok := resp.(oapigen.ListClusters200JSONResponse)
		Expect(ok).To(BeTrue(), "expected ListClusters200JSONResponse")
		Expect(list.Clusters).NotTo(BeNil())
		Expect(*list.Clusters).To(HaveLen(50))
		Expect(list.NextPageToken).NotTo(BeNil())
	})

	It("returns 400 when max_page_size exceeds maximum (TC-HDL-LST-UT-002)", func() {
		req := oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				MaxPageSize: util.Ptr(int32(101)),
			},
		}

		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.ListClusters400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected ListClusters400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 400 when max_page_size is below minimum (TC-HDL-LST-UT-003)", func() {
		req := oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				MaxPageSize: util.Ptr(int32(0)),
			},
		}

		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.ListClusters400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected ListClusters400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns 400 for invalid page_token from service (TC-HDL-LST-UT-004)", func() {
		mock.ListFunc = func(_ context.Context, _ int, _ string) (*v1alpha1.ClusterList, error) {
			return nil, service.NewInvalidArgumentError("invalid page token")
		}

		req := oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				PageToken: util.Ptr("invalid-token"),
			},
		}

		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.ListClusters400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected ListClusters400ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeINVALIDARGUMENT))
	})

	It("returns last page without next_page_token (TC-HDL-LST-UT-005)", func() {
		mock.ListFunc = func(_ context.Context, _ int, _ string) (*v1alpha1.ClusterList, error) {
			return clusterListResult(5, ""), nil
		}

		req := oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				PageToken: util.Ptr("some-page-token"),
			},
		}

		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		list, ok := resp.(oapigen.ListClusters200JSONResponse)
		Expect(ok).To(BeTrue(), "expected ListClusters200JSONResponse")
		Expect(list.Clusters).NotTo(BeNil())
		Expect(*list.Clusters).To(HaveLen(5))
		Expect(list.NextPageToken).To(BeNil())
	})

	It("returns empty collection with empty clusters array (TC-HDL-LST-UT-006)", func() {
		mock.ListFunc = func(_ context.Context, _ int, _ string) (*v1alpha1.ClusterList, error) {
			return clusterListResult(0, ""), nil
		}

		req := oapigen.ListClustersRequestObject{}

		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		list, ok := resp.(oapigen.ListClusters200JSONResponse)
		Expect(ok).To(BeTrue(), "expected ListClusters200JSONResponse")
		Expect(list.Clusters).NotTo(BeNil())
		Expect(*list.Clusters).To(BeEmpty())
		Expect(list.NextPageToken).To(BeNil())
	})

	It("validates max_page_size boundary values (TC-HDL-LST-UT-007)", func() {
		mock.ListFunc = func(_ context.Context, pageSize int, _ string) (*v1alpha1.ClusterList, error) {
			return clusterListResult(pageSize, ""), nil
		}

		By("accepting minimum value of 1")
		req := oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				MaxPageSize: util.Ptr(int32(1)),
			},
		}
		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, ok := resp.(oapigen.ListClusters200JSONResponse)
		Expect(ok).To(BeTrue(), "expected 200 for max_page_size=1")

		By("accepting maximum value of 100")
		req = oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				MaxPageSize: util.Ptr(int32(100)),
			},
		}
		resp, err = h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, ok = resp.(oapigen.ListClusters200JSONResponse)
		Expect(ok).To(BeTrue(), "expected 200 for max_page_size=100")

		By("rejecting value above maximum of 101")
		req = oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				MaxPageSize: util.Ptr(int32(101)),
			},
		}
		resp, err = h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, ok = resp.(oapigen.ListClusters400ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected 400 for max_page_size=101")
	})

	It("treats empty page_token as absent (TC-HDL-LST-UT-008)", func() {
		mock.ListFunc = func(_ context.Context, _ int, _ string) (*v1alpha1.ClusterList, error) {
			return clusterListResult(10, ""), nil
		}

		req := oapigen.ListClustersRequestObject{
			Params: oapigen.ListClustersParams{
				PageToken: util.Ptr(""),
			},
		}

		resp, err := h.ListClusters(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		list, ok := resp.(oapigen.ListClusters200JSONResponse)
		Expect(ok).To(BeTrue(), "expected ListClusters200JSONResponse")
		Expect(list.Clusters).NotTo(BeNil())
	})
})

// ---------------------------------------------------------------------------
// DeleteCluster Handler Tests (5 TCs)
// ---------------------------------------------------------------------------

var _ = Describe("DeleteCluster Handler", func() {
	var (
		h    *handler.Handler
		mock *mockClusterService
		ctx  context.Context
	)

	BeforeEach(func() {
		mock = &mockClusterService{}
		h = handler.New(mock, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		ctx = context.Background()
	})

	It("returns 204 for successful deletion (TC-HDL-DEL-UT-001)", func() {
		mock.DeleteFunc = func(_ context.Context, _ string) error {
			return nil
		}

		req := oapigen.DeleteClusterRequestObject{ClusterId: "cluster-to-delete"}

		resp, err := h.DeleteCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		_, ok := resp.(oapigen.DeleteCluster204Response)
		Expect(ok).To(BeTrue(), "expected DeleteCluster204Response")
	})

	It("returns 404 for non-existent cluster (TC-HDL-DEL-UT-002)", func() {
		mock.DeleteFunc = func(_ context.Context, _ string) error {
			return service.NewNotFoundError("cluster not found")
		}

		req := oapigen.DeleteClusterRequestObject{ClusterId: "non-existent-id"}

		resp, err := h.DeleteCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.DeleteCluster404ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected DeleteCluster404ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeNOTFOUND))
	})

	It("returns 404 for already-deleted cluster (TC-HDL-DEL-UT-003)", func() {
		mock.DeleteFunc = func(_ context.Context, _ string) error {
			return service.NewNotFoundError("cluster already deleted")
		}

		req := oapigen.DeleteClusterRequestObject{ClusterId: "already-deleted-id"}

		resp, err := h.DeleteCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		errResp, ok := resp.(oapigen.DeleteCluster404ApplicationProblemPlusJSONResponse)
		Expect(ok).To(BeTrue(), "expected DeleteCluster404ApplicationProblemPlusJSONResponse")
		Expect(errResp.Type).To(Equal(oapigen.ErrorTypeNOTFOUND))
	})

	It("returns 204 for idempotent delete of DELETING cluster (TC-HDL-DEL-UT-004)", func() {
		mock.DeleteFunc = func(_ context.Context, _ string) error {
			return nil
		}

		req := oapigen.DeleteClusterRequestObject{ClusterId: "deleting-cluster-id"}

		resp, err := h.DeleteCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		_, ok := resp.(oapigen.DeleteCluster204Response)
		Expect(ok).To(BeTrue(), "expected DeleteCluster204Response")
	})

	It("returns DELETING status when getting cluster during deletion (TC-HDL-DEL-UT-005)", func() {
		mock.GetFunc = func(_ context.Context, id string) (*v1alpha1.Cluster, error) {
			result := clusterResult(id)
			result.Status = util.Ptr(v1alpha1.ClusterStatusDELETING)
			return result, nil
		}

		req := oapigen.GetClusterRequestObject{ClusterId: "deleting-cluster-id"}

		resp, err := h.GetCluster(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		cluster, ok := resp.(oapigen.GetCluster200JSONResponse)
		Expect(ok).To(BeTrue(), "expected GetCluster200JSONResponse")
		Expect(cluster.Status).NotTo(BeNil())
		Expect(string(*cluster.Status)).To(Equal("DELETING"))
	})
})

// ---------------------------------------------------------------------------
// Error Mapping Tests (3 TCs)
// ---------------------------------------------------------------------------

var _ = Describe("Error Mapping", func() {
	It("maps error types to correct HTTP status codes (TC-ERR-UT-001)", func() {
		cases := []struct {
			errType    v1alpha1.ErrorType
			wantStatus int
		}{
			{v1alpha1.ErrorTypeINVALIDARGUMENT, 400},
			{v1alpha1.ErrorTypeNOTFOUND, 404},
			{v1alpha1.ErrorTypeALREADYEXISTS, 409},
			{v1alpha1.ErrorTypeUNPROCESSABLEENTITY, 422},
			{v1alpha1.ErrorTypeINTERNAL, 500},
			{v1alpha1.ErrorTypeUNAVAILABLE, 503},
		}

		for _, tc := range cases {
			domainErr := &service.DomainError{Type: tc.errType, Message: "test"}
			errType, status, title, _ := handler.MapDomainError(domainErr)
			Expect(status).To(Equal(tc.wantStatus), "for error type %s", tc.errType)
			Expect(errType).To(Equal(tc.errType), "errType should match input for %s", tc.errType)
			Expect(title).NotTo(BeEmpty(), "title must be non-empty for %s", tc.errType)
		}
	})

	It("does not leak internal details for INTERNAL errors (TC-ERR-UT-002)", func() {
		cause := fmt.Errorf("k8s api error: connection refused to kube-apiserver:6443")
		domainErr := service.NewInternalError("cluster creation failed", cause)

		_, _, _, detail := handler.MapDomainError(domainErr)
		Expect(detail).NotTo(ContainSubstring("k8s"))
		Expect(detail).NotTo(ContainSubstring("kube-apiserver"))
		Expect(detail).NotTo(BeEmpty())
	})

	It("includes detail and instance when provided (TC-ERR-UT-003)", func() {
		domainErr := service.NewNotFoundError("cluster not found").
			WithDetail("cluster abc-123 does not exist")

		errType, status, _, detail := handler.MapDomainError(domainErr)
		Expect(errType).To(Equal(v1alpha1.ErrorTypeNOTFOUND))
		Expect(status).To(Equal(404))
		Expect(detail).To(Equal("cluster abc-123 does not exist"))
	})
})
