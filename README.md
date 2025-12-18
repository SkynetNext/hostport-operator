# HostPort Operator

A production-grade Kubernetes Mutating Webhook designed for workloads requiring high-performance, predictable, and direct network access. It provides deterministic hostPort allocation, effectively replacing the need for complex Service Mesh or LoadBalancer setups in low-latency scenarios.

## Core Capabilities

Inspired by industry standards like **Agones**, this operator provides robust port management that standard Kubernetes lacks:

### 1. Advanced Allocation Policies
- **`Index` (Deterministic)**: Maps ports based on the numeric suffix of the Pod name (e.g., `*-0`, `*-1`). Essential for predictable network topology without external service discovery.
- **`Dynamic` (Pooled)**: Automatically finds the first available port within a specified range on the target node.
- **`Passthrough`**: Directly maps the `containerPort` to the `hostPort`. Ideal for applications that already manage their own port uniqueness.
- **`Static`**: Honors user-defined `hostPort` values in the Pod spec while still providing conflict detection on the node.

### 2. Multi-Port Stride Protection
When a Pod requests multiple ports (e.g., `game`, `metrics`, `admin`), the operator uses a **Stride of 100** for the `Index` policy. This ensures that `app-0` and `app-1` never have overlapping port ranges, even if they occupy multiple ports each.

### 3. Automated Pod Mutation
- **Enforces `hostNetwork: true`**: Automatically enables host networking if the operator is active for the Pod.
- **Spec Correction**: Ensures `containerPort` matches the allocated `hostPort` when using host networking (a Kubernetes requirement for reliable routing).
- **Node-Awareness**: Scans the actual state of the target Node before allocation to guarantee zero physical port conflicts.

### 4. Observability & Audit
Every allocation is written back to the Pod's annotations, providing a clear audit trail of which hostPort was assigned to which container port.

## Annotation Specification

| Annotation | Policy / Value | Description |
|------------|----------------|-------------|
| `hostport.io/enabled` | `true` | **Required**. Activates the operator for this Pod. |
| `hostport.io/policy` | `Index` / `Dynamic` / `Passthrough` / `Static` | Allocation strategy. Defaults to `Index`. |
| `hostport.io/min-port` | Integer | Lower bound of the port range (Default: `7000`). |
| `hostport.io/max-port` | Integer | Upper bound of the port range (Default: `8000`). |

## Usage Example

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: distributed-db
spec:
  template:
    metadata:
      annotations:
        hostport.io/enabled: "true"
        hostport.io/policy: "Index" # Predictable ports based on index
        hostport.io/min-port: "10000"
    spec:
      containers:
      - name: node
        ports:
        - name: primary
          containerPort: 8080 # Allocated to 10000 (db-0), 10001 (db-1), etc.
        - name: metrics
          containerPort: 9090 # Allocated to 10100 (db-0), 10101 (db-1), etc. (Stride 100)
```

## Architecture

1. **Intercept**: Webhook catches the Pod creation request.
2. **Context Discovery**: Extracts index from Pod name and range from annotations.
3. **Node Sync**: Lists existing Pods on the target node to build a "Used Port Map".
4. **Allocate**: Calculates the port based on policy and verifies availability.
5. **Inject**: Mutates the Pod Spec and adds audit annotations.

## Installation

```bash
make deploy IMG=your-registry/hostport-operator:latest
```
