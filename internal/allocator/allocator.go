package allocator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PortPolicy defines how ports are allocated
type PortPolicy string

const (
	PolicyDynamic     PortPolicy = "Dynamic"     // Find any available port in range
	PolicyStatic      PortPolicy = "Static"      // Use hostPort specified in spec
	PolicyPassthrough PortPolicy = "Passthrough" // hostPort == containerPort
	PolicyIndex       PortPolicy = "Index"       // hostPort = minPort + (index * stride) + port_index
)

// Allocator manages hostPort allocation with node-awareness and protocol safety
type Allocator struct {
	mu     sync.Mutex
	client client.Client
	// allocated tracks used ports per node to avoid conflicts
	// Key: nodeName/protocol (e.g. "worker-1/TCP"), Value: set of used ports
	allocated map[string]map[int32]bool
}

func NewAllocator(client client.Client) *Allocator {
	return &Allocator{
		client:    client,
		allocated: make(map[string]map[int32]bool),
	}
}

// PortRequest defines the allocation requirements
type PortRequest struct {
	Name          string
	ContainerPort int32
	HostPort      int32
	Protocol      corev1.Protocol
	Policy        PortPolicy
}

// Allocate performs Agones-aligned port allocation
func (a *Allocator) Allocate(ctx context.Context, pod *corev1.Pod, requests []PortRequest, minPort, maxPort, index, stride int32) ([]PortRequest, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	nodeName := pod.Spec.NodeName
	if nodeName == "" {
		nodeName = "pending"
	}

	// 1. Sync current node state to build the conflict map and find sticky candidates
	stickyPorts, err := a.syncNodeState(ctx, pod, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to sync node state: %w", err)
	}

	results := make([]PortRequest, len(requests))
	for i, req := range requests {
		var allocatedPort int32
		var err error

		protocol := req.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}

		switch req.Policy {
		case PolicyStatic:
			allocatedPort = req.HostPort
			if allocatedPort == 0 {
				return nil, fmt.Errorf("static policy requires hostPort to be set in spec")
			}

		case PolicyPassthrough:
			allocatedPort = req.ContainerPort

		case PolicyIndex:
			// Agones-aligned deterministic stride logic:
			// pod-0 gets [min, min+stride), pod-1 gets [min+stride, min+2*stride)
			allocatedPort = minPort + (index * stride) + int32(i)
			if allocatedPort > maxPort {
				return nil, fmt.Errorf("allocated port %d (index %d, port_idx %d) exceeds max-port %d", allocatedPort, index, i, maxPort)
			}

		case PolicyDynamic:
			// Stickiness Logic:
			// Check if we found historical ports for this POD name during syncNodeState
			foundSticky := false
			if prevPort, exists := stickyPorts[req.Name]; exists {
				// Check if the previous port is still free on THIS node
				if !a.isPortInUse(nodeName, protocol, prevPort) {
					allocatedPort = prevPort
					foundSticky = true
				}
			}

			if !foundSticky {
				allocatedPort, err = a.findFreePort(nodeName, protocol, minPort, maxPort)
				if err != nil {
					return nil, err
				}
			}

		default:
			return nil, fmt.Errorf("unsupported port policy: %s", req.Policy)
		}

		// Conflict check: distinguish between TCP and UDP (Agones feature)
		if a.isPortInUse(nodeName, protocol, allocatedPort) {
			return nil, fmt.Errorf("port %d/%s is already in use on node %s", allocatedPort, protocol, nodeName)
		}

		// Mark as used in local memory to prevent intra-Pod conflicts
		a.markUsed(nodeName, protocol, allocatedPort)

		results[i] = req
		results[i].HostPort = allocatedPort
		results[i].Protocol = protocol
	}

	return results, nil
}

func (a *Allocator) syncNodeState(ctx context.Context, targetPod *corev1.Pod, nodeName string) (map[string]int32, error) {
	// stickyPorts will store ports from an existing pod with the same name (e.g. during rollout)
	stickyPorts := make(map[string]int32)

	// Clear local cache for this node
	a.allocated[nodeName+"/TCP"] = make(map[int32]bool)
	a.allocated[nodeName+"/UDP"] = make(map[int32]bool)

	var podList corev1.PodList
	if err := a.client.List(ctx, &podList, client.InNamespace(targetPod.Namespace)); err != nil {
		return nil, err
	}

	for _, p := range podList.Items {
		// 1. Skip pods on other nodes
		if nodeName != "pending" && p.Spec.NodeName != nodeName {
			continue
		}

		// 2. Identify "Sticky Candidate": A pod with the same name
		// This is usually the old Pod during a StatefulSet RollingUpdate
		isSamePod := p.Name == targetPod.Name

		// 3. Recovery: If it's the same pod name, extract its current allocations as sticky candidates
		if isSamePod {
			for annKey, annVal := range p.Annotations {
				if after, ok := strings.CutPrefix(annKey, "hostport.io/allocated-"); ok {
					if port, err := strconv.Atoi(annVal); err == nil {
						portName := after
						stickyPorts[portName] = int32(port)
					}
				}
			}
		}

		// 4. Conflict Check:
		// If it's the SAME Pod name (and it's being deleted), DON'T mark its ports as "in use"
		// so the new Pod can reclaim them.
		if isSamePod && p.DeletionTimestamp != nil {
			continue
		}

		// Otherwise, mark its ports as occupied
		for _, c := range p.Spec.Containers {
			for _, port := range c.Ports {
				if port.HostPort != 0 {
					proto := string(port.Protocol)
					if proto == "" {
						proto = "TCP"
					}
					key := nodeName + "/" + proto
					if a.allocated[key] == nil {
						a.allocated[key] = make(map[int32]bool)
					}
					a.allocated[key][port.HostPort] = true
				}
			}
		}
	}
	return stickyPorts, nil
}

func (a *Allocator) findFreePort(nodeName string, protocol corev1.Protocol, min, max int32) (int32, error) {
	key := nodeName + "/" + string(protocol)
	for p := min; p <= max; p++ {
		if !a.allocated[key][p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("exhausted available %s ports in range [%d, %d]", protocol, min, max)
}

func (a *Allocator) isPortInUse(nodeName string, protocol corev1.Protocol, port int32) bool {
	key := nodeName + "/" + string(protocol)
	return a.allocated[key][port]
}

func (a *Allocator) markUsed(nodeName string, protocol corev1.Protocol, port int32) {
	key := nodeName + "/" + string(protocol)
	if a.allocated[key] == nil {
		a.allocated[key] = make(map[int32]bool)
	}
	a.allocated[key][port] = true
}
