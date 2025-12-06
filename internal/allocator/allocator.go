package allocator

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Allocator manages hostPort allocation with concurrent safety
// It ensures no two Pods on the same node get the same hostPort
type Allocator struct {
	mu sync.Mutex
	// client is the Kubernetes client for querying existing Pods
	client client.Client
	// allocated tracks allocated ports per node: namespace/nodeIP -> port -> allocated
	allocated map[string]map[int32]bool
}

// NewAllocator creates a new port allocator
func NewAllocator(client client.Client) *Allocator {
	return &Allocator{
		client:    client,
		allocated: make(map[string]map[int32]bool),
	}
}

// AllocatedPorts represents allocated hostPorts
type AllocatedPorts struct {
	Ports []AllocatedPort
}

// AllocatedPort represents a single allocated port
type AllocatedPort struct {
	Name          string
	ContainerPort int32
	HostPort      int32
	Protocol      corev1.Protocol
}

// PortSpec represents a port that needs allocation
type PortSpec struct {
	Name          string
	ContainerPort int32
	Protocol      corev1.Protocol
}

// AllocateForPod allocates hostPorts for a Pod directly
// This is used by the Mutating Webhook
func (a *Allocator) AllocateForPod(ctx context.Context, pod *corev1.Pod, ports []PortSpec, strategyStr string, basePort int32) (*AllocatedPorts, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Get node information
	nodeName := pod.Spec.NodeName
	var nodeIP string

	if nodeName != "" {
		// Pod is pre-scheduled, get node IP
		var node corev1.Node
		if err := a.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
			return nil, fmt.Errorf("failed to get node %s: %w", nodeName, err)
		}
		nodeIP = getNodeIP(&node)
	} else {
		// Pod not scheduled yet, use namespace as key (will be validated when scheduled)
		nodeIP = "pending"
	}

	// Build key for tracking allocations per node
	key := fmt.Sprintf("%s/%s", pod.Namespace, nodeIP)

	// Initialize allocated map for this node if not exists
	if a.allocated[key] == nil {
		a.allocated[key] = make(map[int32]bool)
	}

	// Load existing allocations for this node from actual Pods
	if err := a.loadExistingAllocations(ctx, pod.Namespace, nodeIP, key); err != nil {
		return nil, fmt.Errorf("failed to load existing allocations: %w", err)
	}

	// Exclude current Pod from allocation check (if it already has hostPorts)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.HostPort != 0 {
				// This port is already allocated, don't count it as available
				a.allocated[key][port.HostPort] = true
			}
		}
	}

	// Allocate ports
	var allocatedPorts []AllocatedPort
	for i, portSpec := range ports {
		hostPort := a.allocatePort(key, strategyStr, basePort, i)

		// Mark as allocated
		a.allocated[key][hostPort] = true

		allocatedPorts = append(allocatedPorts, AllocatedPort{
			Name:          portSpec.Name,
			ContainerPort: portSpec.ContainerPort,
			HostPort:      hostPort,
			Protocol:      getProtocol(portSpec.Protocol),
		})
	}

	return &AllocatedPorts{Ports: allocatedPorts}, nil
}

// allocatePort allocates a single port based on strategy
func (a *Allocator) allocatePort(key string, strategyStr string, basePort int32, index int) int32 {
	if strategyStr == "Random" {
		// Random: find any available port starting from basePort
		return a.findNextAvailablePort(key, basePort)
	}

	// Sequential (default): basePort + index (allocate ports sequentially, one per port)
	port := basePort + int32(index)
	if a.allocated[key][port] {
		// Port is taken, find next available
		port = a.findNextAvailablePort(key, port)
	}
	return port
}

// findNextAvailablePort finds the next available port starting from startPort
func (a *Allocator) findNextAvailablePort(key string, startPort int32) int32 {
	port := startPort
	maxPort := startPort + 10000 // Safety limit

	for port < maxPort {
		if !a.allocated[key][port] {
			return port
		}
		port++
	}

	// If we reach here, no port available (shouldn't happen in practice)
	return port
}

// loadExistingAllocations loads existing hostPort allocations from actual Pods
// This ensures we don't conflict with Pods created outside the operator
func (a *Allocator) loadExistingAllocations(ctx context.Context, namespace string, nodeIP, key string) error {
	var podList corev1.PodList
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}

	if err := a.client.List(ctx, &podList, listOpts...); err != nil {
		return err
	}

	for _, pod := range podList.Items {
		// Only consider Pods with hostNetwork
		if !pod.Spec.HostNetwork {
			continue
		}

		// If nodeIP is specified, only consider Pods on that node
		if nodeIP != "pending" && pod.Status.HostIP != nodeIP {
			continue
		}

		// Extract hostPorts from Pod spec
		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.HostPort != 0 {
					a.allocated[key][port.HostPort] = true
				}
			}
		}
	}

	return nil
}

// getNodeIP extracts the internal IP from a Node
func getNodeIP(node *corev1.Node) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	// Fallback to ExternalIP if InternalIP not available
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeExternalIP {
			return addr.Address
		}
	}
	return ""
}

// getProtocol returns the protocol, defaulting to TCP if empty
func getProtocol(protocol corev1.Protocol) corev1.Protocol {
	if protocol == "" {
		return corev1.ProtocolTCP
	}
	return protocol
}
