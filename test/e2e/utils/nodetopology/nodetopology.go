/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nodetopology

import (
	"context"
	"strings"
	"time"

	"github.com/onsi/gomega"

	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	nrtv1alpha2attr "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"
	topologyclientset "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/generated/clientset/versioned"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

const (
	PolicyAttr = "topologyManagerPolicy"
	ScopeAttr  = "topologyManagerScope"

	PolicyDefault = "none"      // TODO: learn somehow from k8s
	ScopeDefault  = "container" // TODO: learn somehow from k8s
)

func GetNodeTopology(topologyClient *topologyclientset.Clientset, nodeName string) *v1alpha2.NodeResourceTopology {
	return GetNodeTopologyWithResource(topologyClient, nodeName, "")
}

func GetNodeTopologyWithResource(topologyClient *topologyclientset.Clientset, nodeName, resName string) *v1alpha2.NodeResourceTopology {
	var nodeTopology *v1alpha2.NodeResourceTopology
	var err error
	gomega.EventuallyWithOffset(1, func() bool {
		nodeTopology, err = topologyClient.TopologyV1alpha2().NodeResourceTopologies().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			framework.Logf("failed to get the node topology resource: %v", err)
			return false
		}
		if resName == "" {
			return true
		}
		return containsResource(nodeTopology, resName)
	}, time.Minute, 5*time.Second).Should(gomega.BeTrue())
	return nodeTopology
}

func isValidAttribute(attrs v1alpha2.AttributeList, name, value string) bool {
	val, ok := nrtv1alpha2attr.Get(attrs, name)
	if !ok {
		framework.Logf("missing expected attribute %q", name)
		return false
	}
	if value != "" && value != val.Value {
		framework.Logf("value mismatch for attribute %q got %q expected %q", name, val.Value, value)
		return false
	}
	return true
}

func IsValidNodeTopology(nodeTopology *v1alpha2.NodeResourceTopology, tmPolicy, tmScope string) bool {
	if nodeTopology == nil {
		framework.Logf("failed to get topology policy from the node topology resource")
		return false
	}

	if len(nodeTopology.TopologyPolicies) > 0 {
		framework.Logf("topologyPolicies is deprecated and should not be populated anymore")
		return false
	}

	if !isValidAttribute(nodeTopology.Attributes, PolicyAttr, tmPolicy) || !isValidAttribute(nodeTopology.Attributes, ScopeAttr, tmScope) {
		// the helper function already logged
		return false
	}

	if nodeTopology.Zones == nil || len(nodeTopology.Zones) == 0 {
		framework.Logf("failed to get topology zones from the node topology resource")
		return false
	}

	foundNodes := 0
	for _, zone := range nodeTopology.Zones {
		// TODO constant not in the APIs
		if !strings.HasPrefix(strings.ToUpper(zone.Type), "NODE") {
			continue
		}
		foundNodes++

		if !IsValidCostList(zone.Name, zone.Costs) {
			framework.Logf("invalid cost list for %q %q", nodeTopology.Name, zone.Name)
			return false
		}

		if !IsValidResourceList(zone.Name, zone.Resources) {
			framework.Logf("invalid resource list for %q %q", nodeTopology.Name, zone.Name)
			return false
		}
	}
	ret := foundNodes > 0
	if !ret {
		framework.Logf("found no Zone with 'node' kind for %q", nodeTopology.Name)
	}
	return ret
}

func IsValidCostList(zoneName string, costs v1alpha2.CostList) bool {
	if len(costs) == 0 {
		framework.Logf("failed to get topology costs for zone %q from the node topology resource", zoneName)
		return false
	}

	// TODO cross-validate zone names
	for _, cost := range costs {
		if cost.Name == "" || cost.Value < 0 {
			framework.Logf("malformed cost %v for zone %q", cost, zoneName)
		}
	}
	return true
}

func IsValidResourceList(zoneName string, resources v1alpha2.ResourceInfoList) bool {
	if len(resources) == 0 {
		framework.Logf("failed to get topology resources for zone %q from the node topology resource", zoneName)
		return false
	}
	foundCpu := false
	for _, resource := range resources {
		if resource.Name == string(corev1.ResourceCPU) {
			foundCpu = true
		}
		available := resource.Available.Value()
		allocatable := resource.Capacity.Value()
		capacity := resource.Capacity.Value()
		if (available < 0 || allocatable < 0 || capacity < 0) || (capacity < available) || (capacity < allocatable) {
			framework.Logf("malformed resource %v for zone %q", resource, zoneName)
			return false
		}
	}
	return foundCpu
}

func AvailableResourceListFromNodeResourceTopology(nodeTopo *v1alpha2.NodeResourceTopology) map[string]corev1.ResourceList {
	availRes := make(map[string]corev1.ResourceList)
	for _, zone := range nodeTopo.Zones {
		if zone.Type != "Node" {
			continue
		}
		resList := make(corev1.ResourceList)
		for _, res := range zone.Resources {
			resList[corev1.ResourceName(res.Name)] = res.Available
		}
		if len(resList) == 0 {
			continue
		}
		availRes[zone.Name] = resList
	}
	return availRes
}

func LessAvailableResources(expected, got map[string]corev1.ResourceList) (string, string, bool) {
	zoneName, resName, cmp, ok := CmpAvailableResources(expected, got)
	if !ok {
		framework.Logf("-> cmp failed (not ok)")
		return "", "", false
	}
	if cmp < 0 {
		return zoneName, resName, true
	}
	framework.Logf("-> cmp failed (value=%d)", cmp)
	return "", "", false
}

func CmpAvailableResources(expected, got map[string]corev1.ResourceList) (string, string, int, bool) {
	if len(got) != len(expected) {
		framework.Logf("-> expected=%v (len=%d) got=%v (len=%d)", expected, len(expected), got, len(got))
		return "", "", 0, false
	}
	for expZoneName, expResList := range expected {
		gotResList, ok := got[expZoneName]
		if !ok {
			return expZoneName, "", 0, false
		}
		if resName, cmp, ok := CmpResourceList(expResList, gotResList); !ok || cmp != 0 {
			return expZoneName, resName, cmp, ok
		}
	}
	return "", "", 0, true
}

func CmpResourceList(expected, got corev1.ResourceList) (string, int, bool) {
	if len(got) != len(expected) {
		framework.Logf("-> expected=%v (len=%d) got=%v (len=%d)", expected, len(expected), got, len(got))
		return "", 0, false
	}
	for expResName, expResQty := range expected {
		gotResQty, ok := got[expResName]
		if !ok {
			return string(expResName), 0, false
		}
		if cmp := gotResQty.Cmp(expResQty); cmp != 0 {
			framework.Logf("-> resource=%q cmp=%d expected=%v got=%v", expResName, cmp, expResQty, gotResQty)
			return string(expResName), cmp, true
		}
	}
	return "", 0, true
}

func CmpAvailableCPUs(expected, got map[string]corev1.ResourceList) (string, int, bool) {
	if len(got) != len(expected) {
		framework.Logf("-> expected=%v (len=%d) got=%v (len=%d)", expected, len(expected), got, len(got))
		return "", 0, false
	}

	for expZoneName, expResList := range expected {
		gotResList, ok := got[expZoneName]
		if !ok {
			return expZoneName, 0, false
		}
		if _, ok := expResList[corev1.ResourceCPU]; !ok {
			framework.Logf("resource=%q does not exist in expected list; expected=%v", corev1.ResourceCPU, expResList)
			return expZoneName, 0, false
		}

		if _, ok := gotResList[corev1.ResourceCPU]; !ok {
			framework.Logf("resource=%q does not exist in got list; got=%v", corev1.ResourceCPU, gotResList)
			return expZoneName, 0, false
		}
		quan := gotResList[corev1.ResourceCPU]
		return "", quan.Cmp(expResList[corev1.ResourceCPU]), true
	}
	return "", 0, true
}

func containsResource(nrt *v1alpha2.NodeResourceTopology, resName string) bool {
	if nrt.Zones == nil || len(nrt.Zones) == 0 {
		framework.Logf("failed to get topology zones from the node topology resource")
		return false
	}

	foundNodes := 0
	for _, zone := range nrt.Zones {
		// TODO constant not in the APIs
		if !strings.HasPrefix(strings.ToUpper(zone.Type), "NODE") {
			continue
		}

		for _, res := range zone.Resources {
			if res.Name == resName {
				framework.Logf("found resource %q in zone %q node %q", resName, zone.Name, nrt.Name)
				foundNodes++
			}
		}
	}

	return foundNodes > 0
}
