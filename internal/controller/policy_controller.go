/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	policyv1alpha1 "kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/engine"
)

// PolicyReconciler reconciles a Policy object
type PolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry engine.PolicyRegistry
}

// +kubebuilder:rbac:groups=policy.k8spe.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.k8spe.io,resources=policies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.k8spe.io,resources=policies/finalizers,verbs=update

func (r *PolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var policyCR policyv1alpha1.Policy
	if err := r.Get(ctx, req.NamespacedName, &policyCR); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Policy deleted", "name", req.Name)
			r.Registry.Delete(req.Name)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch Policy")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	mode := engine.Enforce
	if policyCR.Spec.Mode == "audit" {
		mode = engine.Audit
	}

	p := &engine.Policy{
		Name:      policyCR.Name,
		Mode:      mode,
		Targets:   policyCR.Spec.Targets,
		RegoSrc:   policyCR.Spec.Rego,
		Mutations: policyCR.Spec.Mutations,
		Message:   policyCR.Spec.Message,
		UpdatedAt: time.Now(),
	}

	if err := r.Registry.Set(p); err != nil {
		logger.Error(err, "failed to compile and set policy", "name", req.Name)
		// We shouldn't requeue endlessly on compilation error
		return ctrl.Result{}, nil
	}

	logger.Info("Policy updated", "name", req.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.Policy{}).
		Named("policy").
		Complete(r)
}
