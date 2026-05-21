# End-to-End Testing Guide

## kube-policy-engine

Complete test playbook — from smoke tests to adversarial edge cases.
Run these in order. Each layer assumes the previous layer passes.

---

## Prerequisites

```bash
# Tools required
go 1.22+
kind v0.23+
kubectl v1.29+
helm v3.14+
cert-manager (installed in cluster)

# Start your test cluster
kind create cluster --name kpe-test

# Install cert-manager (required for TLS)
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Available deployment -n cert-manager --all --timeout=60s

# Deploy kube-policy-engine
helm install kpe deploy/helm/kube-policy-engine \
  --namespace kpe-system --create-namespace \
  --set mode=enforce \
  --set logLevel=debug

# Wait for webhook to be ready
kubectl wait --for=condition=Available deployment/kpe-webhook \
  -n kpe-system --timeout=60s

# Confirm webhook is registered
kubectl get validatingwebhookconfigurations
kubectl get mutatingwebhookconfigurations
```

---

## Layer 1 — Health Checks

### 1.1 Webhook server is alive

```bash
kubectl run curl-test --image=curlimages/curl --rm -it --restart=Never -- \
  curl -sk https://kpe-webhook.kpe-system.svc:8443/healthz

# Expected: {"status":"ok"}
```

### 1.2 Metrics endpoint is live

```bash
kubectl port-forward svc/kpe-metrics -n kpe-system 9090:9090 &
curl http://localhost:9090/metrics | grep kpe_

# Expected: kpe_active_policies, kpe_admission_requests_total appear
```

### 1.3 No policies yet — everything passes

```bash
kubectl create namespace layer1-test
kubectl run nginx-test \
  --image=nginx:latest \
  --namespace=layer1-test \
  --overrides='{"spec":{"containers":[{"name":"nginx-test","image":"nginx:latest","securityContext":{"privileged":true}}]}}'

# Expected: Pod CREATED — no policies active yet
kubectl delete namespace layer1-test
```

---

## Layer 2 — CLI Policy Tests (`kpe test`)

Run all built-in policy tests locally before applying anything to the cluster.

```bash
# Run full test suite
go run ./cmd/kpe test ./policies/

# Expected output:
# PASS  require-resource-limits      check-cpu-limit     (3 cases)
# PASS  require-resource-limits      check-memory-limit  (3 cases)
# PASS  no-privileged-containers     deny-privileged     (2 cases)
# PASS  no-latest-tag                deny-latest         (4 cases)
# PASS  require-labels               check-app-label     (3 cases)
# PASS  no-root-user                 deny-uid-zero       (2 cases)
# PASS  no-host-network              deny-host-network   (2 cases)
# PASS  require-probes               check-liveness      (3 cases)
# ─────────────────────────────────────────────────────
# PASS  8 policies, 22 cases (1.24s)

# Verbose mode — shows evaluation trace per case
go run ./cmd/kpe test ./policies/ --verbose
```

### 2.1 Intentionally break a test to verify failure detection

```bash
# Edit a test file to expect the wrong result
sed -i 's/expected: DENY/expected: ALLOW/' policies/no-privileged-containers_test.yaml

go run ./cmd/kpe test ./policies/no-privileged-containers_test.yaml

# Expected: non-zero exit code
# FAIL  no-privileged-containers  deny-privileged  (1 case)
#   case "privileged container should be denied": expected ALLOW got DENY

# Restore
git checkout policies/no-privileged-containers_test.yaml
```

---

## Layer 3 — Validate Policies (Enforce Mode)

Apply each built-in policy one at a time and verify enforcement.

### 3.1 require-resource-limits

```bash
kubectl apply -f policies/require-resource-limits.yaml

# Should FAIL — no resource limits
kubectl run no-limits \
  --image=nginx:1.25 \
  --namespace=default \
  --restart=Never

# Expected: Error from server: admission webhook denied the request
# Message: container no-limits missing cpu limit (policy: require-resource-limits, rule: check-cpu-limit)

# Should PASS — has resource limits
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: with-limits
spec:
  containers:
  - name: nginx
    image: nginx:1.25
    resources:
      limits:
        cpu: "100m"
        memory: "128Mi"
EOF
# Expected: pod/with-limits created

kubectl delete pod with-limits
```

### 3.2 no-privileged-containers

```bash
kubectl apply -f policies/no-privileged-containers.yaml

# Should FAIL
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: privileged-pod
spec:
  containers:
  - name: app
    image: nginx:1.25
    securityContext:
      privileged: true
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
EOF
# Expected: denied — container app must not run as privileged

# Should PASS
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: normal-pod
spec:
  containers:
  - name: app
    image: nginx:1.25
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
EOF
# Expected: pod/normal-pod created
kubectl delete pod normal-pod
```

### 3.3 no-latest-tag

```bash
kubectl apply -f policies/no-latest-tag.yaml

# Should FAIL
kubectl run latest-test --image=nginx:latest --restart=Never
# Expected: denied — image nginx:latest must not use :latest tag

# Should PASS
kubectl run pinned-test --image=nginx:1.25.3 --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"pinned-test","image":"nginx:1.25.3","resources":{"limits":{"cpu":"100m","memory":"128Mi"}}}]}}'
# Expected: created

kubectl delete pod pinned-test
```

### 3.4 require-labels

```bash
kubectl apply -f policies/require-labels.yaml

# Should FAIL — missing labels
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: unlabeled-deploy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: app
        image: nginx:1.25
        resources:
          limits:
            cpu: 100m
            memory: 128Mi
EOF
# Expected: denied — missing required label 'owner'

# Should PASS
# Add metadata.labels.app and metadata.labels.owner to the above
```

### 3.5 Verify all 10 policies pass/fail correctly

```bash
# Run the automated validation script
make test-policies

# This script:
# 1. Applies each policy
# 2. Applies a violating manifest — asserts DENIED
# 3. Applies a compliant manifest — asserts CREATED
# 4. Removes test resources
# Reports PASS/FAIL per policy
```

---

## Layer 4 — Mutating Webhook Tests

### 4.1 read-only-root-fs mutation

```bash
kubectl apply -f policies/read-only-root-fs.yaml

# Apply a pod WITHOUT readOnlyRootFilesystem
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: mutation-test
  labels:
    app: test
    owner: dev
spec:
  containers:
  - name: app
    image: nginx:1.25
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
    # No securityContext set
EOF

# Inspect the created pod — mutation should have added readOnlyRootFilesystem
kubectl get pod mutation-test -o jsonpath='{.spec.containers[0].securityContext}'
# Expected: {"readOnlyRootFilesystem":true}

kubectl delete pod mutation-test
```

### 4.2 add-managed-by-label mutation

```bash
kubectl apply -f policies/add-managed-by-label.yaml

kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: label-mutation-test
  labels:
    app: test
    owner: dev
spec:
  containers:
  - name: app
    image: nginx:1.25
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
EOF

kubectl get pod label-mutation-test -o jsonpath='{.metadata.labels}'
# Expected: {"app":"test","owner":"dev","app.kubernetes.io/managed-by":"kpe"}

kubectl delete pod label-mutation-test
```

### 4.3 default-resource-limits mutation

```bash
kubectl apply -f policies/default-resource-limits.yaml

# First disable require-resource-limits so we can test defaults separately
kubectl patch policy require-resource-limits \
  --type=merge -p '{"spec":{"enabled":false}}'

# Apply pod with no limits
kubectl run defaults-test --image=nginx:1.25 \
  --labels="app=test,owner=dev" --restart=Never

kubectl get pod defaults-test \
  -o jsonpath='{.spec.containers[0].resources}'
# Expected: {"limits":{"cpu":"100m","memory":"128Mi"}}

kubectl delete pod defaults-test

# Re-enable
kubectl patch policy require-resource-limits \
  --type=merge -p '{"spec":{"enabled":true}}'
```

---

## Layer 5 — Audit Mode

### 5.1 Global audit mode

```bash
# Restart webhook in audit-only mode
helm upgrade kpe deploy/helm/kube-policy-engine \
  -n kpe-system \
  --set mode=audit

# Now a violating pod SHOULD be allowed through
kubectl run audit-test \
  --image=nginx:latest \
  --restart=Never

# Expected: pod/audit-test created (NOT denied)

# But violation should appear in Kubernetes Events
kubectl get events --field-selector reason=PolicyViolation
# Expected: events showing policy violations for audit-test pod

# And in Prometheus metrics
curl http://localhost:9090/metrics | grep kpe_policy_violations_total
# Expected: counter incremented

kubectl delete pod audit-test
helm upgrade kpe deploy/helm/kube-policy-engine \
  -n kpe-system --set mode=enforce
```

### 5.2 Per-policy audit mode

```bash
# Switch just no-latest-tag to Audit mode, keep others in Enforce
kubectl patch policy no-latest-tag \
  --type=merge -p '{"spec":{"mode":"Audit"}}'

# This should now be ALLOWED (audit only)
kubectl run audit-only-test --image=nginx:latest \
  --labels="app=test,owner=dev" \
  --overrides='{"spec":{"containers":[{"name":"audit-only-test","image":"nginx:latest","resources":{"limits":{"cpu":"100m","memory":"128Mi"}}}]}}'

# Expected: pod created
# But events show PolicyViolation for no-latest-tag

kubectl get events --field-selector reason=PolicyViolation | grep no-latest-tag
# Expected: event present

# This should still be DENIED (other policies still enforcing)
kubectl run no-limits-denied --image=nginx:1.25 --restart=Never
# Expected: denied by require-resource-limits

kubectl delete pod audit-only-test

# Restore
kubectl patch policy no-latest-tag \
  --type=merge -p '{"spec":{"mode":"Enforce"}}'
```

---

## Layer 6 — Hot Reload

```bash
# Watch policy count in real time
watch kubectl get policies

# In another terminal: add a new policy
cat > /tmp/new-policy.yaml <<EOF
apiVersion: policy.kpe.io/v1alpha1
kind: Policy
metadata:
  name: no-host-pid
spec:
  enabled: true
  mode: Enforce
  match:
    kinds:
      - kind: Pod
        apiVersion: v1
  rules:
    - name: deny-host-pid
      type: Validate
      message: "Pods must not use hostPID"
      rego: |
        package kpe
        deny[msg] {
          input.spec.hostPID == true
          msg := "pod must not use hostPID"
        }
EOF

kubectl apply -f /tmp/new-policy.yaml

# Wait 5 seconds — policy should load without restart
sleep 5

# Verify policy is active (no webhook restart)
kubectl get policy no-host-pid
# Expected: policy appears

# Test it works
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: hostpid-test
  labels:
    app: test
    owner: dev
spec:
  hostPID: true
  containers:
  - name: app
    image: nginx:1.25
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
EOF
# Expected: denied — pod must not use hostPID

# Confirm webhook was NOT restarted during the test
kubectl get pods -n kpe-system
# Check the AGE column — should be > 5 minutes (no restart)

# Delete the test policy
kubectl delete policy no-host-pid
sleep 5

# Confirm the policy is no longer enforced
kubectl apply -f - <<EOF
... same hostPID pod ...
EOF
# Expected: created (policy gone)
kubectl delete pod hostpid-test
```

---

## Layer 7 — Prometheus Metrics Verification

```bash
kubectl port-forward svc/kpe-metrics -n kpe-system 9090:9090 &

# Generate some traffic
kubectl run metrics-test-pass \
  --image=nginx:1.25 \
  --labels="app=test,owner=dev" \
  --overrides='{"spec":{"containers":[{"name":"metrics-test-pass","image":"nginx:1.25","resources":{"limits":{"cpu":"100m","memory":"128Mi"}}}]}}'

kubectl run metrics-test-fail --image=nginx:latest

# Check all metrics are populated
curl -s http://localhost:9090/metrics | grep -E 'kpe_'

# Verify specific metrics:
# kpe_admission_requests_total{result="allowed"} should be > 0
# kpe_admission_requests_total{result="denied"} should be > 0
# kpe_policy_violations_total should be > 0
# kpe_webhook_duration_seconds has observations
# kpe_active_policies > 0

curl -s http://localhost:9090/metrics \
  | grep kpe_admission_requests_total
# Expected:
# kpe_admission_requests_total{kind="Pod",operation="CREATE",result="allowed"} 1
# kpe_admission_requests_total{kind="Pod",operation="CREATE",result="denied"} 1

kubectl delete pod metrics-test-pass
```

---

## Layer 8 — Edge Cases & Adversarial Tests

### 8.1 Malformed YAML — webhook must not crash

```bash
# Send a directly malformed admission review (bypasses kubectl)
curl -sk -X POST https://localhost:8443/validate \
  -H "Content-Type: application/json" \
  -d '{"not": "a valid admission review"}'

# Expected: 400 Bad Request with error message
# Expected: webhook server still running after this call
kubectl get pods -n kpe-system  # should still be Running
```

### 8.2 Invalid Rego syntax — rejected at policy creation

```bash
kubectl apply -f - <<EOF
apiVersion: policy.kpe.io/v1alpha1
kind: Policy
metadata:
  name: bad-rego
spec:
  enabled: true
  mode: Enforce
  match:
    kinds:
      - kind: Pod
        apiVersion: v1
  rules:
    - name: broken
      type: Validate
      rego: |
        package kpe
        this is not valid rego {{{{
EOF

# Expected: Error from server — invalid Rego syntax: ...
# Policy CRD should NOT be created
kubectl get policy bad-rego
# Expected: Error from server (NotFound)
```

### 8.3 Policy with no matching resources — no performance impact

```bash
kubectl apply -f - <<EOF
apiVersion: policy.kpe.io/v1alpha1
kind: Policy
metadata:
  name: match-nothing
spec:
  enabled: true
  mode: Enforce
  match:
    kinds:
      - kind: NonExistentResource
        apiVersion: fake.io/v1
  rules:
    - name: dummy
      type: Validate
      rego: |
        package kpe
        deny[msg] { msg := "never triggers" }
EOF

# Apply a normal pod — should not be slowed down by the non-matching policy
time kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: perf-test
  labels:
    app: test
    owner: dev
spec:
  containers:
  - name: app
    image: nginx:1.25
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
EOF
# Expected: real < 1s (non-matching policy adds no latency)

kubectl delete pod perf-test
kubectl delete policy match-nothing
```

### 8.4 Disable a policy mid-flight

```bash
# Apply a policy
kubectl apply -f policies/no-privileged-containers.yaml

# Confirm it blocks
kubectl apply -f - <<EOF
...privileged pod...
EOF
# Expected: denied

# Disable the policy
kubectl patch policy no-privileged-containers \
  --type=merge -p '{"spec":{"enabled":false}}'

sleep 5  # wait for hot reload

# Same pod should now be allowed
kubectl apply -f - <<EOF
...same privileged pod...
EOF
# Expected: created

kubectl delete pod ...
```

### 8.5 Concurrent policy reloads don't cause errors

```bash
# Apply 5 policies rapidly in parallel
for i in 1 2 3 4 5; do
  kubectl apply -f policies/ &
done
wait

# While applying, hammer the webhook with admission requests
for i in $(seq 1 20); do
  kubectl run concurrent-test-$i \
    --image=nginx:1.25 \
    --labels="app=test,owner=dev" \
    --overrides="..." &
done
wait

# Expected: no 500 errors, no panics in webhook logs
kubectl logs -n kpe-system deployment/kpe-webhook | grep -i "panic\|fatal\|error"
# Expected: no panic or fatal lines
```

---

## Layer 9 — envtest Integration Tests

```bash
# Run the full envtest suite (spins up a real API server in-process)
make test

# What these tests cover:
# - TestValidateAllow: compliant pod gets allowed
# - TestValidateDeny: non-compliant pod gets denied with correct message
# - TestMutateDefaultLimits: missing limits are patched in
# - TestMutateReadOnlyRootFS: securityContext is patched
# - TestHotReload: policy change reflected within 5s
# - TestAuditMode: violation logged but request allowed
# - TestInvalidRego: bad rego rejected at policy creation
# - TestConcurrentRequests: 100 concurrent requests with no race conditions

go test ./... -race -count=1 -timeout=120s
# -race flag catches any data races in the hot reload RWMutex
```

---

## Layer 10 — Load Test (Webhook Latency)

```bash
# Install hey (HTTP load tester)
go install github.com/rakyll/hey@latest

# Generate 1000 admission requests and measure latency
# (Use the test webhook endpoint, not the real Kubernetes admission path)
hey -n 1000 -c 10 \
  -m POST \
  -H "Content-Type: application/json" \
  -D tests/fixtures/valid-admission-review.json \
  https://localhost:8443/validate

# Expected results:
# p50: < 5ms
# p95: < 20ms
# p99: < 50ms
# No errors

# Record these numbers for your resume bullet:
# "p99 webhook latency < 50ms under 1000 concurrent requests"
```

---

## Layer 11 — Full Demo Script (record this as a GIF)

This is the sequence to record for your README and LFX cover letter.

```bash
# Terminal split: left = commands, right = kubectl get pods --watch

# Step 1: Show policies are active
kubectl get policies

# Step 2: Try to create a bad pod (should fail with clear message)
kubectl run demo-bad \
  --image=nginx:latest \
  --restart=Never
# Camera on: "denied — image must not use :latest tag"

# Step 3: Show audit mode catching a violation without blocking
kubectl patch policy no-latest-tag --type=merge -p '{"spec":{"mode":"Audit"}}'
kubectl run demo-audit --image=nginx:latest --restart=Never
# Camera on: pod created, then show:
kubectl get events --field-selector reason=PolicyViolation

# Step 4: Show hot reload — add a new policy live
kubectl apply -f /tmp/new-policy.yaml
# Camera on: policy appears in 'kubectl get policies' within 5s
# Test it works immediately

# Step 5: Show Prometheus metrics
curl http://localhost:9090/metrics | grep kpe_policy_violations_total
# Camera on: counter has incremented

# Step 6: Show kpe test CLI
go run ./cmd/kpe test ./policies/
# Camera on: all tests passing in < 2s
```

---

## Common Failures & Fixes

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Webhook times out (10s) | cert-manager cert not ready | `kubectl get certificate -n kpe-system` — wait for Ready |
| All requests denied | Policy match is too broad | Check `match.kinds` — `apiVersion: "*"` matches everything |
| Hot reload not working | Informer cache sync delay | Increase `--reload-interval` to 10s for debugging |
| `kpe test` passes but cluster denies | Rego evaluates `input` differently | Run `kpe test --verbose` to compare input shape |
| Metrics not incrementing | Wrong label cardinality | Check Prometheus label names match the metric definition |
| envtest fails with "no matches for kind" | CRDs not installed in envtest | Add CRD paths to `envtest.Environment.CRDDirectoryPaths` |
