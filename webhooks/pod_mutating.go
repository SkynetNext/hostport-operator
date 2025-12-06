package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/SkynetNext/hostport-operator/internal/allocator"
)

const (
	// AnnotationHostPortEnabled indicates that hostPort allocation is enabled for this Pod
	AnnotationHostPortEnabled = "hostport.io/enabled"
	// AnnotationHostPortBasePort specifies the base port for allocation
	AnnotationHostPortBasePort = "hostport.io/base-port"
	// AnnotationHostPortStrategy specifies the allocation strategy
	AnnotationHostPortStrategy = "hostport.io/strategy"
	// AnnotationHostPortAllocatedPrefix is the prefix for annotations storing allocated ports
	AnnotationHostPortAllocatedPrefix = "hostport.io/allocated-"
)

// PodMutator mutates Pods to inject allocated hostPorts
type PodMutator struct {
	Client    client.Client
	decoder   *admission.Decoder
	allocator *allocator.Allocator
}

// NewPodMutator creates a new Pod mutator
func NewPodMutator(client client.Client, scheme *runtime.Scheme, alloc *allocator.Allocator) *PodMutator {
	return &PodMutator{
		Client:    client,
		decoder:   admission.NewDecoder(scheme),
		allocator: alloc,
	}
}

// Handle handles admission requests for Pods
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := m.decoder.Decode(req, pod); err != nil {
		logger.Error(err, "Failed to decode Pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if hostPort allocation is enabled
	enabled := pod.Annotations[AnnotationHostPortEnabled]
	if enabled != "true" {
		return admission.Allowed("hostPort allocation not enabled")
	}

	// Ensure hostNetwork is enabled
	if !pod.Spec.HostNetwork {
		pod.Spec.HostNetwork = true
		logger.Info("Enabled hostNetwork for Pod", "pod", pod.Name)
	}

	// Get allocation parameters from annotations
	basePort := int32(30558) // default
	if basePortStr := pod.Annotations[AnnotationHostPortBasePort]; basePortStr != "" {
		if _, err := fmt.Sscanf(basePortStr, "%d", &basePort); err != nil {
			logger.Error(err, "Invalid base port annotation", "value", basePortStr)
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("invalid base port: %s", basePortStr))
		}
	}

	strategy := "Sequential" // default
	if strategyStr := pod.Annotations[AnnotationHostPortStrategy]; strategyStr != "" {
		strategy = strategyStr
	}

	// Extract ports that need hostPort allocation
	portsToAllocate := make([]allocator.PortSpec, 0)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			// Only allocate hostPort if containerPort is specified but hostPort is not
			if port.ContainerPort != 0 && port.HostPort == 0 {
				portsToAllocate = append(portsToAllocate, allocator.PortSpec{
					Name:          port.Name,
					ContainerPort: port.ContainerPort,
					Protocol:      port.Protocol,
				})
			}
		}
	}

	// If no ports need allocation, still return the mutated Pod (with hostNetwork enabled)
	if len(portsToAllocate) == 0 {
		logger.Info("No ports need allocation, but hostNetwork is enabled", "pod", pod.Name)
		// Marshal mutated Pod to JSON (with hostNetwork: true)
		marshaledPod, err := json.Marshal(pod)
		if err != nil {
			logger.Error(err, "Failed to marshal mutated Pod")
			return admission.Errored(http.StatusInternalServerError, err)
		}
		return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
	}

	// Allocate ports
	allocatedPorts, err := m.allocator.AllocateForPod(ctx, pod, portsToAllocate, strategy, basePort)
	if err != nil {
		logger.Error(err, "Failed to allocate ports")
		return admission.Denied(fmt.Sprintf("Failed to allocate hostPorts: %v", err))
	}

	// Inject allocated hostPorts into Pod
	if err := m.injectHostPorts(pod, allocatedPorts); err != nil {
		logger.Error(err, "Failed to inject hostPorts")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Add annotations to track allocated ports
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	for _, port := range allocatedPorts.Ports {
		pod.Annotations[fmt.Sprintf("%s%s", AnnotationHostPortAllocatedPrefix, port.Name)] = fmt.Sprintf("%d", port.HostPort)
	}

	logger.Info("Allocated hostPorts for Pod", "pod", pod.Name, "ports", allocatedPorts.Ports)

	// Marshal mutated Pod to JSON
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Failed to marshal mutated Pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Return mutated Pod
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// injectHostPorts injects allocated hostPorts into Pod container ports
// When hostNetwork is true, hostPort must equal containerPort (Kubernetes requirement)
func (m *PodMutator) injectHostPorts(pod *corev1.Pod, allocatedPorts *allocator.AllocatedPorts) error {
	portMap := make(map[string]allocator.AllocatedPort)
	for _, port := range allocatedPorts.Ports {
		portMap[port.Name] = port
	}

	// Update container ports with allocated hostPorts
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		for j := range container.Ports {
			port := &container.Ports[j]
			if allocatedPort, ok := portMap[port.Name]; ok {
				port.HostPort = allocatedPort.HostPort
				// When hostNetwork is true, containerPort must equal hostPort
				// So we set containerPort to the allocated hostPort
				port.ContainerPort = allocatedPort.HostPort
				port.Protocol = allocatedPort.Protocol
			}
		}
	}

	return nil
}

// SetupWithManager sets up the webhook with the manager
func SetupWithManager(mgr ctrl.Manager, alloc *allocator.Allocator) error {
	mutator := NewPodMutator(mgr.GetClient(), mgr.GetScheme(), alloc)
	mgr.GetWebhookServer().Register("/mutate-pods", &webhook.Admission{
		Handler: mutator,
	})
	return nil
}
