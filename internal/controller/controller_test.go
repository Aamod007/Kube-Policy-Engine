package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	policyv1alpha1 "kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/engine"
)

func TestPolicyReconciler(t *testing.T) {
	sch := runtime.NewScheme()
	_ = policyv1alpha1.AddToScheme(sch)

	registry := engine.NewRegistry(engine.NewEngine())

	policy := &policyv1alpha1.Policy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
		Spec: policyv1alpha1.PolicySpec{
			Mode: "enforce",
			Rego: "package k8spe\ndeny[msg] { false; msg := \"\" }",
		},
	}

	client := fake.NewClientBuilder().WithScheme(sch).WithObjects(policy).Build()

	r := &PolicyReconciler{
		Client:   client,
		Scheme:   sch,
		Registry: registry,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-policy",
			Namespace: "default",
		},
	}

	// Test add
	res, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)

	p, ok := registry.Get("test-policy")
	assert.True(t, ok)
	assert.Equal(t, engine.Enforce, p.Mode)
	assert.NotNil(t, p.Compiled)

	// Test delete
	err = client.Delete(context.Background(), policy)
	assert.NoError(t, err)

	res, err = r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)

	_, ok = registry.Get("test-policy")
	assert.False(t, ok)
}
