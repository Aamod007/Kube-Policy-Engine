# Kube-Policy-Engine

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.30+-326CE5?logo=kubernetes)](https://kubernetes.io/)
[![License](https://img.shields.io/badge/License-Apache--2.0-green)](LICENSE)
[![OPA](https://img.shields.io/badge/Policy%20Engine-OPA%2FRego-7d9199)](https://www.openpolicyagent.org/)

A production-grade Kubernetes admission controller with a built-in OPA/Rego policy engine. Enforce guardrails, apply best practices, and mutate resources in real-time -- all through native Kubernetes Custom Resources.

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
  - [Prerequisites](#prerequisites)
  - [Deploy to a Cluster](#deploy-to-a-cluster)
- [Writing Policies](#writing-policies)
  - [Policy CRD Reference](#policy-crd-reference)
  - [Built-in Mutations](#built-in-mutations)
- [CLI Reference](#cli-reference)
- [Metrics & Observability](#metrics--observability)
- [Development](#development)
  - [Project Structure](#project-structure)
  - [Makefile Targets](#makefile-targets)
  - [Running Locally](#running-locally)
- [Contributing](#contributing)
- [License](#license)

---

## Features

| Feature | Description |
|---|---|
| **Validating Webhook** | Intercepts `CREATE` and `UPDATE` operations, evaluating Rego policies against resources in real-time |
| **Mutating Webhook** | Applies RFC 6902 JSON patches to resources -- inject labels, set defaults, rewrite image tags |
| **Rego-Powered CRDs** | Define policies as native Kubernetes `Policy` resources with embedded OPA/Rego scripts |
| **Hot-Reload Engine** | Zero-downtime policy updates -- changes take effect within seconds via controller-runtime watches |
| **Audit & Enforce Modes** | Test policies silently in `audit` mode, then switch to `enforce` to block non-compliant resources |
| **Fail-Open / Fail-Closed** | Configurable behavior for webhook errors to balance availability vs. security |
| **Offline CLI Testing** | Lint, evaluate, and test policies against JSON/YAML manifests before deploying to a cluster |
| **Prometheus Metrics** | Structured metrics for violations, evaluations, latency, and active policies with Grafana dashboards |
| **TLS Hot-Reload** | fsnotify-based certificate reloading -- no restarts when cert-manager rotates secrets |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                           │
│                                                                     │
│  ┌──────────────┐    ┌──────────────────────────────────────────┐   │
│  │  kubectl /   │    │          kube-policy-engine              │   │
│  │  Controllers │    │                                          │   │
│  └──────┬───────┘    │  ┌────────────────────────────────────┐  │   │
│         │            │  │       Admission Webhook Server     │  │   │
│         │ HTTPS      │  │  /validate  /mutate  /healthz      │  │   │
│         ▼            │  └────────────────┬───────────────────┘  │   │
│  ┌──────────────┐    │                   │                       │   │
│  │   API Server  │───▶│  ┌──────────────▼───────────────────┐  │   │
│  │  (Admission  │    │  │        Policy Registry            │  │   │
│  │   Hooks)     │    │  │  (Thread-safe in-memory cache)    │  │   │
│  └──────────────┘    │  └──────────────┬───────────────────┘  │   │
│                      │                   │                       │   │
│  ┌──────────────┐    │  ┌──────────────▼───────────────────┐  │   │
│  │  cert-manager │───▶│  │        OPA / Rego Engine        │  │   │
│  │  (TLS certs)  │    │  │  Compile & Evaluate Policies    │  │   │
│  └──────────────┘    │  └───────────────────────────────────┘  │   │
│                      └──────────────────────────────────────────┘   │
│                                                                     │
│  ┌──────────────┐                                                   │
│  │  Policy CRs  │──▶ controller-runtime reconciler syncs to registry│
│  └──────────────┘                                                   │
└─────────────────────────────────────────────────────────────────────┘
```

**Request Flow:**

1. Kubernetes API Server receives a resource `CREATE` or `UPDATE`
2. Admission webhook configuration routes the request to `kube-policy-engine` over HTTPS (TLS via cert-manager)
3. The handler deserializes the `AdmissionReview`, matches applicable policies by target (apiGroup, resource, operation)
4. Each matching policy is evaluated via the OPA/Rego engine against the request payload
5. **Enforce mode**: violations return a denial response with human-readable messages
6. **Audit mode**: violations are recorded in Prometheus metrics but the request is allowed
7. **Mutating webhook**: applicable mutations generate RFC 6902 JSON patches
8. All operations increment Prometheus counters observable on `:9090/metrics`

---

## Quick Start

### Prerequisites

| Tool | Version | Purpose |
|---|---|---|
| [Go](https://golang.org/doc/install) | 1.22+ | Build the engine and CLI |
| [Helm](https://helm.sh/docs/intro/install/) | 3.x | Deploy to a cluster |
| [kind](https://kind.sigs.k8s.io/) | latest | Local Kubernetes cluster |
| [cert-manager](https://cert-manager.io/docs/installation/) | latest | TLS certificate management |

### Deploy to a Cluster

**1. Create a local cluster and install cert-manager**

```bash
kind create cluster --config deploy/kind-config.yaml

kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Available deploy -n cert-manager --all --timeout=120s
```

**2. Deploy Kube-Policy-Engine via Helm**

```bash
helm install kpe deploy/helm/kube-policy-engine \
  --namespace kpe-system \
  --create-namespace
```

**3. Verify the deployment**

```bash
kubectl get pods -n kpe-system
kubectl get policies -A
```

**4. Apply a sample policy**

```bash
kubectl apply -f policies/require-pod-labels/policy.yaml
```

---

## Writing Policies

Policies are defined as standard Kubernetes `Policy` Custom Resources. Each policy contains a Rego script that evaluates admission requests.

### Example: Deny Privileged Containers

```yaml
apiVersion: policy.k8spe.io/v1alpha1
kind: Policy
metadata:
  name: no-privileged-containers
spec:
  mode: enforce
  message: "Privileged containers are not allowed"
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

### Policy CRD Reference

| Field | Type | Required | Description |
|---|---|---|---|
| `spec.mode` | `string` | Yes | `enforce` (block) or `audit` (log only) |
| `spec.message` | `string` | No | Custom denial message shown to users |
| `spec.targets` | `[]Target` | Yes | Resources and operations this policy applies to |
| `spec.targets[].apiGroups` | `[]string` | Yes | API groups (use `""` for core, `"*"` for all) |
| `spec.targets[].resources` | `[]string` | Yes | Resource types (e.g., `pods`, `deployments`, `"*"`) |
| `spec.targets[].operations` | `[]string` | Yes | Operations: `CREATE`, `UPDATE`, `"*"` |
| `spec.rego` | `string` | Yes | OPA/Rego script defining the policy logic |
| `spec.mutations` | `[]string` | No | Built-in mutation names to apply |

### Built-in Mutations

| Mutation | Description | Resource Scope |
|---|---|---|
| `inject-managed-label` | Adds `app.kubernetes.io/managed-by: kube-policy-engine` label | All |
| `set-default-resources` | Sets CPU `50m` and memory `64Mi` requests on containers | Pods only |
| `rewrite-latest-tag` | Rewrites `:latest` image tags to `:stable` | Pods only |

---

## CLI Reference

The bundled `policy` CLI enables offline policy development and testing.

```bash
# Build the CLI
go build -o bin/policy ./cmd/policy
```

| Command | Description |
|---|---|
| `policy lint <path>` | Validate policy YAML structure and Rego syntax |
| `policy test <path>` | Run test suites from `test.yaml` files |
| `policy eval --policy <name> --input <file>` | Evaluate a single manifest against a policy |

### Test Suite Format

```yaml
policy: no-privileged-containers
cases:
  - name: "deny privileged pod"
    input: ./privileged-pod.json
    expect: deny
    message: "Privileged containers are not allowed"

  - name: "allow non-privileged pod"
    input: ./safe-pod.json
    expect: allow
```

---

## Metrics & Observability

All metrics are exposed on `:9090/metrics` in Prometheus format.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `policy_violations_total` | Counter | `policy`, `resource`, `namespace`, `mode` | Total policy violations |
| `policy_evaluations_total` | Counter | `policy`, `result` | Total evaluations (allow/deny) |
| `policy_errors_total` | Counter | `policy` | Evaluation errors |
| `webhook_request_duration_ms` | Histogram | `operation`, `resource` | Webhook latency (buckets: 1-500ms) |
| `active_policies` | Gauge | `mode` | Number of loaded policies |

A preconfigured Grafana dashboard is available at `deploy/dashboards/violations.json`.

---

## Development

### Project Structure

```
├── api/v1alpha1/              # Policy CRD types and registration
├── cli/                       # CLI commands (eval, lint, test)
├── cmd/
│   ├── server/main.go         # Admission controller entry point
│   └── policy/main.go         # CLI entry point
├── config/
│   ├── crd/bases/             # Generated CRD manifests
│   └── rbac/                  # Generated RBAC rules
├── deploy/
│   ├── helm/                  # Helm chart with cert-manager TLS
│   ├── dashboards/            # Grafana dashboard templates
│   └── kind-config.yaml       # Kind cluster configuration
├── internal/
│   ├── admission/             # Validating & mutating webhook handlers
│   ├── controller/            # Policy CRD reconciler
│   ├── engine/                # OPA/Rego evaluation engine & registry
│   ├── metrics/               # Prometheus metric definitions
│   └── tls/                   # Hot-reloading TLS certificate loader
├── policies/                  # Sample policies with test suites
└── tests/e2e/                 # End-to-end test suite
```

### Makefile Targets

| Target | Description |
|---|---|
| `make build` | Compile the server and CLI binaries |
| `make run` | Run the controller locally against a cluster |
| `make test` | Run unit tests with envtest |
| `make test-e2e` | Run end-to-end tests on a Kind cluster |
| `make lint` | Lint Go code with golangci-lint |
| `make lint-fix` | Auto-fix linting issues |
| `make generate` | Regenerate DeepCopy methods |
| `make manifests` | Regenerate CRDs and RBAC from markers |
| `make docker-build` | Build the container image |
| `make docker-push` | Push the container image to a registry |
| `make deploy` | Deploy the controller to a cluster |
| `make undeploy` | Remove the controller from a cluster |

### Running Locally

```bash
# Start the controller against your current kubeconfig context
make run

# In another terminal, apply a policy
kubectl apply -f policies/require-pod-labels/policy.yaml
```

---

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Commit your changes (`git commit -m 'feat: add my feature'`)
4. Push to the branch (`git push origin feat/my-feature`)
5. Open a Pull Request

---

## License

Apache License 2.0 -- see [LICENSE](LICENSE) for details.
