package admission_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/admission"
	"kube-policy-engine/internal/engine"
)

func TestMutateWebhook(t *testing.T) {
	eng := engine.NewEngine()
	registry := engine.NewRegistry(eng)

	policy := &engine.Policy{
		Name: "test-mutations",
		Mode: engine.Enforce,
		RegoSrc: `package k8spe
deny[msg] { false; msg := "" }`,
		Targets: []v1alpha1.Target{
			{
				APIGroups:  []string{""},
				Resources:  []string{"pods"},
				Operations: []string{"CREATE"},
			},
		},
		Mutations: []string{"inject-managed-label", "set-default-resources", "rewrite-latest-tag"},
	}
	err := registry.Set(policy)
	assert.NoError(t, err)

	h := &admission.Handler{
		Registry: registry,
		Engine:   eng,
	}

	podJSON := `{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "test-pod"
		},
		"spec": {
			"containers": [
				{
					"name": "nginx",
					"image": "nginx:latest"
				}
			]
		}
	}`

	req := &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Object:    runtime.RawExtension{Raw: []byte(podJSON)},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/mutate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeMutate(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var res admissionv1.AdmissionReview
	err = json.Unmarshal(w.Body.Bytes(), &res)
	assert.NoError(t, err)

	assert.True(t, res.Response.Allowed)
	assert.NotNil(t, res.Response, "Response should not be nil")

	if res.Response.Patch == nil {
		t.Fatalf("Response Patch should not be nil, actual response: %+v", res.Response)
	}

	assert.NotNil(t, res.Response.PatchType, "Response PatchType should not be nil")
	assert.Equal(t, admissionv1.PatchTypeJSONPatch, *res.Response.PatchType)

	var patches []jsonpatch.Operation
	err = json.Unmarshal(res.Response.Patch, &patches)
	assert.NoError(t, err)

	assert.Len(t, patches, 3) // Add labels, Add requests object, Replace image
}

func TestValidateWebhook(t *testing.T) {
	regoSrc := `
package k8spe
deny[msg] {
	input.request.object.spec.privileged == true
	msg := "Privileged is not allowed"
}
`
	eng := engine.NewEngine()
	registry := engine.NewRegistry(eng)

	policy := &engine.Policy{
		Name:    "test-policy",
		Mode:    engine.Enforce,
		RegoSrc: regoSrc,
		Targets: []v1alpha1.Target{
			{
				APIGroups:  []string{""},
				Resources:  []string{"pods"},
				Operations: []string{"CREATE"},
			},
		},
	}
	err := registry.Set(policy)
	assert.NoError(t, err)

	h := &admission.Handler{
		Registry: registry,
		Engine:   eng,
	}

	podJSON := `{"spec": {"privileged": true}}`

	req := &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Object:    runtime.RawExtension{Raw: []byte(podJSON)},
		},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeValidate(w, httpReq)

	assert.Equal(t, http.StatusOK, w.Code)

	var res admissionv1.AdmissionReview
	err = json.Unmarshal(w.Body.Bytes(), &res)
	assert.NoError(t, err)

	assert.False(t, res.Response.Allowed)
	assert.Equal(t, "Privileged is not allowed", res.Response.Result.Message)
}
