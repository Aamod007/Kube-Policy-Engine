package admission

import (
	"encoding/json"
	"fmt"
	"strings"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
)

func (h *Handler) mutate(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	policies := h.Registry.List()
	if len(policies) == 0 {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	var patches []jsonpatch.Operation

	// Fast track if there are no mutations active
	activeMutations := make(map[string]bool)
	for _, policy := range policies {
		if !matchesTargets(policy.Targets, req) {
			continue
		}
		for _, m := range policy.Mutations {
			activeMutations[m] = true
		}
	}

	if len(activeMutations) == 0 {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	// Parse the incoming object to apply mutations
	var obj map[string]any
	if err := json.Unmarshal(req.Object.Raw, &obj); err != nil {
		// Log and allow for mutate phase
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	if activeMutations["inject-managed-label"] {
		patches = append(patches, injectManagedLabel(obj)...)
	}

	if activeMutations["set-default-resources"] && isPod(req) {
		var pod corev1.Pod
		if err := json.Unmarshal(req.Object.Raw, &pod); err == nil {
			patches = append(patches, setDefaultResources(pod)...)
		}
	}

	if activeMutations["rewrite-latest-tag"] && isPod(req) {
		var pod corev1.Pod
		if err := json.Unmarshal(req.Object.Raw, &pod); err == nil {
			patches = append(patches, rewriteLatestTag(pod)...)
		}
	}

	if len(patches) == 0 {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	pt := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		PatchType: &pt,
		Patch:     patchBytes,
	}
}

func isPod(req *admissionv1.AdmissionRequest) bool {
	return req.Resource.Resource == "pods" && req.Kind.Kind == "Pod"
}

func injectManagedLabel(obj map[string]any) []jsonpatch.Operation {
	var ops []jsonpatch.Operation

	metadata, ok := obj["metadata"].(map[string]any)
	if !ok {
		metadata = make(map[string]any)
		ops = append(ops, jsonpatch.NewOperation("add", "/metadata", metadata))
	}

	labels, ok := metadata["labels"].(map[string]any)
	if !ok {
		ops = append(ops, jsonpatch.NewOperation("add", "/metadata/labels", map[string]any{"app.kubernetes.io/managed-by": "kube-policy-engine"}))
		return ops
	}

	if _, exists := labels["app.kubernetes.io/managed-by"]; !exists {
		ops = append(ops, jsonpatch.NewOperation("add", "/metadata/labels/app.kubernetes.io~1managed-by", "kube-policy-engine"))
	}
	return ops
}

func setDefaultResources(pod corev1.Pod) []jsonpatch.Operation {
	var ops []jsonpatch.Operation

	// Need to check if containers array exists at all
	if len(pod.Spec.Containers) == 0 {
		return ops
	}

	for i, c := range pod.Spec.Containers {
		if c.Resources.Requests == nil {
			// If Requests is nil, we add the entire requests object
			ops = append(ops, jsonpatch.NewOperation("add", fmt.Sprintf("/spec/containers/%d/resources/requests", i), map[string]string{
				"cpu":    "50m",
				"memory": "64Mi",
			}))
		} else {
			if c.Resources.Requests.Cpu().IsZero() {
				ops = append(ops, jsonpatch.NewOperation("add", fmt.Sprintf("/spec/containers/%d/resources/requests/cpu", i), "50m"))
			}
			if c.Resources.Requests.Memory().IsZero() {
				ops = append(ops, jsonpatch.NewOperation("add", fmt.Sprintf("/spec/containers/%d/resources/requests/memory", i), "64Mi"))
			}
		}
	}
	return ops
}

func rewriteLatestTag(pod corev1.Pod) []jsonpatch.Operation {
	var ops []jsonpatch.Operation

	for i, c := range pod.Spec.Containers {
		if strings.HasSuffix(c.Image, ":latest") {
			newImage, _ := strings.CutSuffix(c.Image, ":latest")
			newImage += ":stable"
			ops = append(ops, jsonpatch.NewOperation("replace", fmt.Sprintf("/spec/containers/%d/image", i), newImage))
		}
	}
	return ops
}
