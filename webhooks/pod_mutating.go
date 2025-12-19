package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/SkynetNext/hostport-operator/internal/allocator"
	"github.com/SkynetNext/hostport-operator/internal/metrics"
)

const (
	AnnotationEnabled         = "hostport.io/enabled"
	AnnotationPolicy          = "hostport.io/policy"
	AnnotationMinPort         = "hostport.io/min-port"
	AnnotationMaxPort         = "hostport.io/max-port"
	AnnotationStride          = "hostport.io/stride"
	AnnotationAllocatedPrefix = "hostport.io/allocated-"
)

type PodMutator struct {
	Client    client.Client
	decoder   *admission.Decoder
	allocator *allocator.Allocator
}

func NewPodMutator(client client.Client, scheme *runtime.Scheme, alloc *allocator.Allocator) *PodMutator {
	return &PodMutator{
		Client:    client,
		decoder:   admission.NewDecoder(scheme),
		allocator: alloc,
	}
}

func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)
	pod := &corev1.Pod{}
	if err := m.decoder.Decode(req, pod); err != nil {
		metrics.WebhookRequestsTotal.WithLabelValues("errored").Inc()
		return admission.Errored(http.StatusBadRequest, err)
	}

	if pod.Annotations[AnnotationEnabled] != "true" {
		metrics.WebhookRequestsTotal.WithLabelValues("allowed").Inc()
		return admission.Allowed("hostPort allocation not enabled")
	}

	// 1. Configuration Parsing
	minPort := int32(7000)
	if val, ok := pod.Annotations[AnnotationMinPort]; ok {
		if i, err := strconv.Atoi(val); err == nil {
			minPort = int32(i)
		}
	}

	maxPort := int32(8000)
	if val, ok := pod.Annotations[AnnotationMaxPort]; ok {
		if i, err := strconv.Atoi(val); err == nil {
			maxPort = int32(i)
		}
	}

	stride := int32(10) // Default stride per Pod (Agones-aligned)
	if val, ok := pod.Annotations[AnnotationStride]; ok {
		if i, err := strconv.Atoi(val); err == nil {
			stride = int32(i)
		}
	}

	policy := allocator.PolicyIndex
	if val, ok := pod.Annotations[AnnotationPolicy]; ok {
		policy = allocator.PortPolicy(val)
	}

	// 2. Extract Numeric Index from Name (app-0, app-1...)
	index := int32(0)
	name := pod.Name
	if name == "" {
		name = pod.GenerateName
	}
	if lastDash := strings.LastIndex(name, "-"); lastDash != -1 {
		if o, err := strconv.Atoi(name[lastDash+1:]); err == nil {
			index = int32(o)
		}
	}

	// 3. Collect Port Requests
	var portRequests []allocator.PortRequest
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.HostPort == 0 && port.ContainerPort != 0 {
				portRequests = append(portRequests, allocator.PortRequest{
					Name:          port.Name,
					ContainerPort: port.ContainerPort,
					Protocol:      port.Protocol,
					Policy:        policy,
				})
			}
		}
	}

	if len(portRequests) == 0 {
		metrics.WebhookRequestsTotal.WithLabelValues("allowed").Inc()
		return admission.Allowed("no ports need allocation")
	}

	// 4. Perform Allocation with Protocol and Stride Awareness
	allocated, err := m.allocator.Allocate(ctx, pod, portRequests, minPort, maxPort, index, stride)
	if err != nil {
		logger.Error(err, "Port allocation failed")
		metrics.WebhookRequestsTotal.WithLabelValues("denied").Inc()
		return admission.Denied(err.Error())
	}

	// 5. Apply Mutations
	if !pod.Spec.HostNetwork {
		pod.Spec.HostNetwork = true
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	for _, a := range allocated {
		m.applyToSpec(pod, a)
		pod.Annotations[AnnotationAllocatedPrefix+a.Name] = fmt.Sprintf("%d", a.HostPort)
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		metrics.WebhookRequestsTotal.WithLabelValues("errored").Inc()
		return admission.Errored(http.StatusInternalServerError, err)
	}

	metrics.WebhookRequestsTotal.WithLabelValues("allowed").Inc()
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (m *PodMutator) applyToSpec(pod *corev1.Pod, alloc allocator.PortRequest) {
	for i := range pod.Spec.Containers {
		for j := range pod.Spec.Containers[i].Ports {
			p := &pod.Spec.Containers[i].Ports[j]
			// Match by name or by original containerPort
			if p.Name == alloc.Name || (p.Name == "" && p.ContainerPort == alloc.ContainerPort) {
				p.HostPort = alloc.HostPort
				// For hostNetwork, containerPort should be updated to match allocated hostPort
				p.ContainerPort = alloc.HostPort
			}
		}
	}
}

func SetupWithManager(mgr ctrl.Manager, alloc *allocator.Allocator) error {
	mutator := NewPodMutator(mgr.GetClient(), mgr.GetScheme(), alloc)
	mgr.GetWebhookServer().Register("/mutate-pods", &webhook.Admission{
		Handler: mutator,
	})
	return nil
}
