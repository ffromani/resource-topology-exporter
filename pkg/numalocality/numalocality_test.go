package numalocality

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"
)

func TestGetNUMAIDs(t *testing.T) {
	type testCase struct {
		name                  string
		topo                  *podresourcesapi.TopologyInfo
		expectedUniqueNUMAIDs []int
	}

	testCases := []testCase{
		{
			name: "nil",
			//topo:                  nil,
			expectedUniqueNUMAIDs: []int{},
		},
		{
			name:                  "nil nodes",
			topo:                  &podresourcesapi.TopologyInfo{},
			expectedUniqueNUMAIDs: []int{},
		},
		{
			name: "empty nodes",
			topo: &podresourcesapi.TopologyInfo{
				Nodes: []*podresourcesapi.NUMANode{},
			},
			expectedUniqueNUMAIDs: []int{},
		},
		{
			name: "any NUMA locality",
			topo: &podresourcesapi.TopologyInfo{
				Nodes: []*podresourcesapi.NUMANode{
					{
						ID: -1,
					},
				},
			},
			expectedUniqueNUMAIDs: []int{},
		},
		{
			name: "defined single NUMA locality",
			topo: &podresourcesapi.TopologyInfo{
				Nodes: []*podresourcesapi.NUMANode{
					{ID: 1},
					{ID: -1},
				},
			},
			expectedUniqueNUMAIDs: []int{1},
		},
		{
			name: "defined multiple NUMA localities with duplicates",
			topo: &podresourcesapi.TopologyInfo{
				Nodes: []*podresourcesapi.NUMANode{
					{ID: 1},
					{ID: 2},
					{ID: 1},
				},
			},
			expectedUniqueNUMAIDs: []int{1, 2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetNUMAIDs(tc.topo)
			if diff := cmp.Diff(tc.expectedUniqueNUMAIDs, got); diff != "" {
				t.Fatalf("expected=%v got=%v diff=%s", tc.expectedUniqueNUMAIDs, got, diff)
			}
		})
	}
}
