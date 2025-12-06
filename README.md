# HostPort Operator

A Kubernetes Mutating Webhook for automatic hostPort allocation, solving concurrent port conflicts in Pod deployments.

## Overview

HostPort Operator provides automatic hostPort allocation via a **Mutating Webhook**. When you create a Pod (via StatefulSet, Deployment, etc.) with the `hostport.io/enabled: "true"` annotation, the webhook automatically allocates available hostPorts and injects them into the Pod spec.

## Features

- 🔒 **Concurrent Safe**: Global mutex ensures no hostPort conflicts
- 🎯 **Automatic Allocation**: Mutating Webhook automatically injects hostPorts
- 🚀 **Works with Standard Resources**: Use StatefulSet, Deployment, DaemonSet, etc.
- 📋 **User Control**: You manage Pod lifecycle, Operator only handles ports

## Architecture

```
┌─────────────────┐
│  StatefulSet    │  (User creates this)
│  / Deployment   │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│      Pod        │  (With annotation: hostport.io/enabled: "true")
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Mutating Webhook│  (Intercepts Pod creation)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Allocator     │  (Allocates ports, ensures no conflicts)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Pod Created   │  (With allocated hostPorts injected)
└─────────────────┘
```

## Installation

### Prerequisites

- Kubernetes 1.20+
- kubectl configured to access your cluster
- Go 1.21+ (for building from source)

### Deploy the Operator

1. **Deploy the Operator**:
   ```bash
   make deploy
   ```

2. **Verify Installation**:
   ```bash
   kubectl get pods -n operators -l app.kubernetes.io/name=hostport-operator
   kubectl get mutatingwebhookconfiguration hostport-operator-hostport-mutating-webhook-configuration
   ```

## Usage

### Create a Pod with Automatic hostPort Allocation

Simply add the annotation `hostport.io/enabled: "true"` to your Pod template:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: game-server
spec:
  replicas: 2
  template:
    metadata:
      annotations:
        hostport.io/enabled: "true"        # Enable hostPort allocation
        hostport.io/base-port: "30558"     # Optional: base port (default: 30558)
        hostport.io/strategy: "Sequential" # Optional: Sequential or Random (default: Sequential)
    spec:
      hostNetwork: true                     # Required: must be true
      containers:
      - name: game
        image: my-game-server:latest
        ports:
        - name: grpc
          containerPort: 19805
          # hostPort will be automatically allocated by the webhook
        - name: health
          containerPort: 9090
          # hostPort will be automatically allocated by the webhook
```

### Annotation Reference

| Annotation | Description | Default | Required |
|------------|-------------|---------|----------|
| `hostport.io/enabled` | Enable hostPort allocation | - | Yes (must be `"true"`) |
| `hostport.io/base-port` | Starting port number for allocation | `30558` | No |
| `hostport.io/strategy` | Allocation strategy (`Sequential` or `Random`) | `Sequential` | No |

### Check Allocated Ports

After Pod creation, check the annotations to see allocated ports:

```bash
kubectl get pod <pod-name> -o jsonpath='{.metadata.annotations}'
```

The allocated ports are stored in annotations:
- `hostport.io/allocated-<port-name>`: The allocated hostPort number

### Example

See `config/samples/statefulset-example.yaml` for a complete example.

## How It Works

1. **User creates Pod** (via StatefulSet, Deployment, etc.) with `hostport.io/enabled: "true"` annotation
2. **Mutating Webhook intercepts** the Pod creation request
3. **Allocator checks** existing Pods on the same node to find available ports
4. **Ports are allocated** based on the specified strategy (Sequential or Random)
5. **hostPort values are injected** into the Pod spec
6. **Pod is created** with allocated hostPorts

## Allocation Strategies

- **Sequential**: Allocates ports sequentially based on index (deterministic)
  - Pod 0: 30558, 30559
  - Pod 1: 30560, 30561
  - etc.

- **Random**: Allocates ports randomly from available pool
  - Finds first available port starting from basePort

## Development

### Prerequisites

- Go 1.21+
- kubebuilder tools (installed via Makefile)

### Build

```bash
# Generate RBAC manifests
make manifests

# Build the manager binary
make build

# Run tests
make test
```

### Run Locally

```bash
make run
```

### Build and Push Docker Image

```bash
export IMG=your-registry/hostport-operator:tag
make docker-build
make docker-push
```

### Deploy

```bash
# Deploy to cluster
make deploy
```

## Troubleshooting

### Check Operator Logs

```bash
kubectl logs -n operators -l app.kubernetes.io/name=hostport-operator
```

### Check Webhook Configuration

```bash
kubectl get mutatingwebhookconfiguration hostport-operator-hostport-mutating-webhook-configuration -o yaml
```

### Verify Pod Annotations

```bash
kubectl get pod <pod-name> -o yaml | grep hostport.io
```

### Common Issues

1. **Webhook not called**: Check MutatingWebhookConfiguration and Service
2. **Port conflict**: Operator automatically finds next available port
3. **hostNetwork not set**: Webhook automatically sets `hostNetwork: true` if annotation is present

## License

This project is licensed under the Apache 2.0 License.
