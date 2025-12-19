package allocator

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAllocator_IndexPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	alloc := NewAllocator(fakeClient)

	tests := []struct {
		name      string
		podName   string
		index     int32
		stride    int32
		minPort   int32
		maxPort   int32
		portCount int
		wantPorts []int32
	}{
		{
			name:      "pod-0 with single port",
			podName:   "app-0",
			index:     0,
			stride:    10,
			minPort:   7000,
			maxPort:   8000,
			portCount: 1,
			wantPorts: []int32{7000},
		},
		{
			name:      "pod-1 with single port",
			podName:   "app-1",
			index:     1,
			stride:    10,
			minPort:   7000,
			maxPort:   8000,
			portCount: 1,
			wantPorts: []int32{7010},
		},
		{
			name:      "pod-0 with multiple ports",
			podName:   "app-0",
			index:     0,
			stride:    100,
			minPort:   7000,
			maxPort:   8000,
			portCount: 3,
			wantPorts: []int32{7000, 7001, 7002},
		},
		{
			name:      "pod-1 with multiple ports (stride 100)",
			podName:   "app-1",
			index:     1,
			stride:    100,
			minPort:   7000,
			maxPort:   8000,
			portCount: 3,
			wantPorts: []int32{7100, 7101, 7102},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.podName,
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-1",
				},
			}

			requests := make([]PortRequest, tt.portCount)
			for i := 0; i < tt.portCount; i++ {
				portName := "port" + string(rune('a'+i))
				requests[i] = PortRequest{
					Name:          portName,
					ContainerPort: int32(8080 + i),
					Protocol:      corev1.ProtocolTCP,
					Policy:        PolicyIndex,
				}
			}

			ctx := context.Background()
			result, err := alloc.Allocate(ctx, pod, requests, tt.minPort, tt.maxPort, tt.index, tt.stride)

			if err != nil {
				t.Fatalf("Allocate() error = %v", err)
			}

			if len(result) != tt.portCount {
				t.Fatalf("Allocate() returned %d ports, want %d", len(result), tt.portCount)
			}

			for i, wantPort := range tt.wantPorts {
				if result[i].HostPort != wantPort {
					t.Errorf("Allocate() result[%d].HostPort = %d, want %d", i, result[i].HostPort, wantPort)
				}
			}
		})
	}
}

func TestAllocator_PassthroughPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	alloc := NewAllocator(fakeClient)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-0",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	requests := []PortRequest{
		{
			Name:          "http",
			ContainerPort: 8080,
			Protocol:      corev1.ProtocolTCP,
			Policy:        PolicyPassthrough,
		},
		{
			Name:          "metrics",
			ContainerPort: 9090,
			Protocol:      corev1.ProtocolTCP,
			Policy:        PolicyPassthrough,
		},
	}

	ctx := context.Background()
	result, err := alloc.Allocate(ctx, pod, requests, 7000, 8000, 0, 10)

	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	if result[0].HostPort != 8080 {
		t.Errorf("Allocate() result[0].HostPort = %d, want 8080", result[0].HostPort)
	}

	if result[1].HostPort != 9090 {
		t.Errorf("Allocate() result[1].HostPort = %d, want 9090", result[1].HostPort)
	}
}

func TestAllocator_StaticPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	alloc := NewAllocator(fakeClient)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-0",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	requests := []PortRequest{
		{
			Name:          "http",
			ContainerPort: 8080,
			HostPort:      10000,
			Protocol:      corev1.ProtocolTCP,
			Policy:        PolicyStatic,
		},
	}

	ctx := context.Background()
	result, err := alloc.Allocate(ctx, pod, requests, 7000, 8000, 0, 10)

	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	if result[0].HostPort != 10000 {
		t.Errorf("Allocate() result[0].HostPort = %d, want 10000", result[0].HostPort)
	}
}

func TestAllocator_StaticPolicy_MissingHostPort(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	alloc := NewAllocator(fakeClient)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-0",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	requests := []PortRequest{
		{
			Name:          "http",
			ContainerPort: 8080,
			HostPort:      0, // Missing hostPort
			Protocol:      corev1.ProtocolTCP,
			Policy:        PolicyStatic,
		},
	}

	ctx := context.Background()
	_, err := alloc.Allocate(ctx, pod, requests, 7000, 8000, 0, 10)

	if err == nil {
		t.Error("Allocate() expected error for missing hostPort in Static policy, got nil")
	}
}

func TestAllocator_IndexPolicy_ExceedsMaxPort(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	alloc := NewAllocator(fakeClient)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-100",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	requests := []PortRequest{
		{
			Name:          "http",
			ContainerPort: 8080,
			Protocol:      corev1.ProtocolTCP,
			Policy:        PolicyIndex,
		},
	}

	ctx := context.Background()
	// index=100, stride=10, minPort=7000 -> port = 7000 + 100*10 = 8000 (exceeds maxPort=7999)
	_, err := alloc.Allocate(ctx, pod, requests, 7000, 7999, 100, 10)

	if err == nil {
		t.Error("Allocate() expected error for exceeding maxPort, got nil")
	}
}

func TestAllocator_UnsupportedPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	alloc := NewAllocator(fakeClient)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-0",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	requests := []PortRequest{
		{
			Name:          "http",
			ContainerPort: 8080,
			Protocol:      corev1.ProtocolTCP,
			Policy:        PortPolicy("Invalid"),
		},
	}

	ctx := context.Background()
	_, err := alloc.Allocate(ctx, pod, requests, 7000, 8000, 0, 10)

	if err == nil {
		t.Error("Allocate() expected error for unsupported policy, got nil")
	}
}

func TestAllocator_ProtocolSeparation(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)

	// Create a pod with TCP port already allocated
	existingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8080,
							HostPort:      7000,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingPod).Build()
	alloc := NewAllocator(fakeClient)

	// Try to allocate UDP on the same port (should succeed - different protocol)
	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
		},
	}

	requests := []PortRequest{
		{
			Name:          "udp",
			ContainerPort: 8080,
			Protocol:      corev1.ProtocolUDP,
			Policy:        PolicyPassthrough,
		},
	}

	ctx := context.Background()
	result, err := alloc.Allocate(ctx, newPod, requests, 7000, 8000, 0, 10)

	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// UDP port 7000 should be allocated even though TCP 7000 is in use
	if result[0].HostPort != 8080 {
		t.Errorf("Allocate() result[0].HostPort = %d, want 8080 (UDP should use containerPort)", result[0].HostPort)
	}
}
