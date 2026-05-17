# Kube-Policy-Engine

A production-grade Kubernetes admission controller featuring a built-in Rego policy engine.

**Kube-Policy-Engine** provides developers and cluster operators with a lightweight, robust, and fast policy-as-code solution designed to enforce guardrails and establish best practices without the heavy operational overhead associated with more complex deployments.

## Features

- **Validating and Mutating Webhooks**: Intercepts `CREATE` and `UPDATE` resource operations directly, evaluating rules against cluster objects in real-time.
- **Rego-Powered CRD Policies**: Policies are defined as standard Kubernetes `Policy` Custom Resources incorporating OPA/Rego scripts, keeping definitions clean and tightly coupled to the K8s ecosystem.
- **Zero-Downtime Hot-Reloads**: Fully integrated with the `controller-runtime`, the engine watches active policies and re-evaluates configurations seamlessly. Changes to policies take effect within seconds—no server restarts necessary.
- **Audit Mode vs. Enforce Mode**: Validate policies silently in `audit` mode to collect violations without rejecting requests or actively block non-compliant resources with `enforce` mode.
- **Testing CLI**: A powerful bundled CLI (`policy test ./policies/`) enables developers to test rules against offline JSON/YAML Kubernetes manifests, accelerating policy CI/CD testing.
- **Prometheus Metrics**: Exposes structured health and violation metrics on a dedicated Prometheus endpoint, seamlessly viewable in the bundled Grafana dashboards.
- **Hardcoded Extensible Mutations**: Simplify mutation pipelines via easily toggled flags for defaults such as automatically setting resource quotas, rewriting tags, or injecting labels.

---

## Architecture Overview

1. **Kubernetes API Server** triggers `ValidatingAdmissionWebhook` or `MutatingAdmissionWebhook` configuration rules.
2. Webhooks forward HTTPS admission requests to the `kube-policy-engine` backend, operating with TLS certificates generated dynamically via **cert-manager**.
3. Internal handler parses requests using `OPA/Rego` inside an efficiently maintained in-memory `PolicyRegistry`.
4. Rejected payloads issue human-readable errors, while permitted and mutated traffic gets formatted into standard RFC 6902 JSON Patches.
5. All operations increment localized Prometheus counters natively observable on port `:9090`.

---

## Getting Started

### Prerequisites

- [Go 1.22+](https://golang.org/doc/install)
- [kind](https://kind.sigs.k8s.io/) (for local cluster testing)
- [cert-manager](https://cert-manager.io/docs/installation/)
- [Helm 3](https://helm.sh/docs/intro/install/)

### Local Cluster Deployment

1. **Start the local cluster and install cert-manager**
   ```bash
   kind create cluster --config deploy/kind-config.yaml
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
   kubectl wait --for=condition=Available deploy -n cert-manager --all --timeout=120s
   ```

2. **Deploy the Engine via Helm**
   The repository includes a ready-to-deploy Helm chart:
   ```bash
   helm install kpe deploy/helm/kube-policy-engine --namespace kpe-system --create-namespace
   ```

3. **Check deployment status**
   ```bash
   kubectl get pods -n kpe-system
   kubectl get policies
   ```

---

## Developing Policies

Policies can be crafted as standard Kubernetes YAML objects. Example to deny privileged containers:

```yaml
apiVersion: policy.k8spe.io/v1alpha1
kind: Policy
metadata:
  name: no-privileged-containers
spec:
  mode: enforce
  targets:
    - apiGroups: [""]
      resources: ["pods"]
      operations: ["CREATE", "UPDATE"]
  rego: |
    package k8spe
    deny[msg] {
      input.request.object.spec.containers[_].securityContext.privileged == true
      msg := "Privileged containers are not allowed"
    }
```

### Local CLI Testing

Use the bundled `policy` CLI to safely vet rules before hitting production.

```bash
# Build the CLI tool
go build -o bin/policy ./cmd/policy

# Lint policy file configurations
./bin/policy lint ./policies/no-privileged-containers

# Execute all automated policy tests
./bin/policy test ./policies/

# Test a single manifest evaluation payload
./bin/policy eval --policy no-privileged-containers --input deploy/pod.yaml
```

---

## Metrics & Observability

The `kube-policy-engine` exposes vital operations and metrics via the `:9090/metrics` endpoint. Available metrics include:

- `policy_violations_total`: Tracking violation triggers (categorized by policy name, namespace, mode).
- `policy_evaluations_total`: Successful evaluations counters.
- `webhook_request_duration_ms`: High-fidelity latency histograms.
- `active_policies`: Number of parsed and loaded policies currently active.

A preconfigured Grafana dashboard template is provided in `deploy/dashboards/violations.json`.

---

## Development & Makefile Tasks

Useful tasks to compile and format the engine:

```bash
make test           # Run unit tests and envtest
make lint           # Lint the Go codebase using golangci-lint
make generate       # Rebuild operator-sdk/kubebuilder configuration manifests
make test-e2e       # Provision a Kind environment and assert E2E interactions
```
