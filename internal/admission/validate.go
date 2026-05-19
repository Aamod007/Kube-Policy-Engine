package admission

import (
	"context"
	"encoding/json"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/engine"
	"kube-policy-engine/internal/metrics"
)

func (h *Handler) validate(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	policies := h.Registry.List()
	if len(policies) == 0 {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	// Prepare input for OPA
	var obj map[string]any
	if err := json.Unmarshal(req.Object.Raw, &obj); err != nil {
		return errorResponse(fmt.Errorf("failed to unmarshal request object: %v", err), h.FailOpen)
	}

	input := map[string]any{
		"request": map[string]any{
			"object":    obj,
			"namespace": req.Namespace,
			"operation": string(req.Operation),
			"kind":      req.Kind,
			"userInfo":  req.UserInfo,
		},
	}

	for _, policy := range policies {
		if !matchesTargets(policy.Targets, req) {
			continue
		}

		res, err := h.Engine.Eval(context.Background(), policy, input)
		if err != nil {
			metrics.PolicyErrorsTotal.WithLabelValues(policy.Name).Inc()
			if !h.FailOpen {
				return errorResponse(fmt.Errorf("policy evaluation failed: %v", err), false)
			}
			continue
		}

		if !res.Allowed {
			metrics.PolicyEvaluationsTotal.WithLabelValues(policy.Name, "deny").Inc()
			metrics.PolicyViolationsTotal.WithLabelValues(policy.Name, req.Resource.Resource, req.Namespace, string(policy.Mode)).Inc()

			msg := policy.Message
			if len(res.Deny) > 0 && res.Deny[0] != "" {
				msg = res.Deny[0]
			}

			if policy.Mode == engine.Enforce {
				return &admissionv1.AdmissionResponse{
					Allowed: false,
					Result: &metav1.Status{
						Message: msg,
						Code:    403,
					},
				}
			}
			// In Audit mode, we just record the violation and continue evaluating
		} else {
			metrics.PolicyEvaluationsTotal.WithLabelValues(policy.Name, "allow").Inc()
		}
	}

	return &admissionv1.AdmissionResponse{Allowed: true}
}

func matchesTargets(targets []v1alpha1.Target, req *admissionv1.AdmissionRequest) bool {
	if len(targets) == 0 {
		return true // No targets = match all (maybe dangerous, but per schema)
	}

	for _, target := range targets {
		groupMatch := false
		for _, g := range target.APIGroups {
			if g == "*" || g == req.Kind.Group {
				groupMatch = true
				break
			}
		}

		resMatch := false
		for _, r := range target.Resources {
			if r == "*" || r == req.Resource.Resource {
				resMatch = true
				break
			}
		}

		opMatch := false
		for _, o := range target.Operations {
			if o == "*" || o == string(req.Operation) {
				opMatch = true
				break
			}
		}

		if groupMatch && resMatch && opMatch {
			return true
		}
	}
	return false
}
