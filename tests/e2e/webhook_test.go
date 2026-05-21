package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"kube-policy-engine/api/v1alpha1"
)

func TestE2E(t *testing.T) {
	t.Skip("E2E tests require a running kind cluster. Skipping for unit tests.")
	// A placeholder struct to represent what the e2e test will do when run against the real cluster
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	k8sClient, err := client.New(cfg, client.Options{})
	require.NoError(t, err)

	dynClient, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Mutating Webhook Injects Label", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-mutate-",
				Namespace:    "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "nginx",
						Image: "nginx:latest",
					},
				},
			},
		}

		err := k8sClient.Create(ctx, pod)
		require.NoError(t, err)
		defer func() { _ = k8sClient.Delete(ctx, pod) }()

		// Wait for pod to be created
		time.Sleep(1 * time.Second)

		var createdPod corev1.Pod
		err = k8sClient.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, &createdPod)
		require.NoError(t, err)

		// Assert mutations applied
		assert.Equal(t, "kube-policy-engine", createdPod.Labels["app.kubernetes.io/managed-by"])
		assert.Equal(t, "nginx:stable", createdPod.Spec.Containers[0].Image)
		assert.Equal(t, "50m", createdPod.Spec.Containers[0].Resources.Requests.Cpu().String())
	})

	t.Run("Validating Webhook Rejects Privileged", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-validate-",
				Namespace:    "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "nginx",
						Image: "nginx:stable",
						SecurityContext: &corev1.SecurityContext{
							Privileged: ptrBool(true),
						},
					},
				},
			},
		}

		err := k8sClient.Create(ctx, pod)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Privileged containers are not allowed")
	})

	// Add hot-reload test by modifying policy CRD via dynamic client and testing effect
	_ = dynClient
	_ = v1alpha1.Policy{}
}

func ptrBool(b bool) *bool {
	return &b
}
