package admission

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"kube-policy-engine/internal/engine"
	"kube-policy-engine/internal/metrics"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

type Handler struct {
	Registry engine.PolicyRegistry
	Engine   engine.Engine
	FailOpen bool
}

func (h *Handler) ServeValidate(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, h.validate)
}

func (h *Handler) ServeMutate(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, h.mutate)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, admitFunc func(*admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse) {
	logger := log.FromContext(r.Context())

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(err, "could not read request body")
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return
	}

	var admissionReview admissionv1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		logger.Error(err, "could not deserialize request")
		http.Error(w, "could not deserialize request", http.StatusBadRequest)
		return
	}

	if admissionReview.Request == nil {
		logger.Info("malformed admission review: request is nil")
		http.Error(w, "malformed admission review: request is nil", http.StatusBadRequest)
		return
	}

	start := time.Now()

	admissionResponse := admitFunc(admissionReview.Request)

	duration := time.Since(start)
	metrics.WebhookRequestDuration.WithLabelValues(string(admissionReview.Request.Operation), admissionReview.Request.Resource.Resource).Observe(float64(duration.Milliseconds()))

	admissionReview.Response = admissionResponse
	if admissionReview.Response != nil {
		admissionReview.Response.UID = admissionReview.Request.UID
	}

	admissionReview.Request = nil // Don't send request back

	respBytes, err := json.Marshal(admissionReview)
	if err != nil {
		logger.Error(err, "could not serialize response")
		http.Error(w, "could not serialize response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(respBytes); err != nil {
		logger.Error(err, "could not write response")
	}
}

func errorResponse(err error, allow bool) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: allow,
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}
