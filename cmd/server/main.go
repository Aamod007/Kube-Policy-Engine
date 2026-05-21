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

package main

import (
	"crypto/tls"
	"flag"
	"net/http"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	policyv1alpha1 "kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/admission"
	"kube-policy-engine/internal/controller"
	"kube-policy-engine/internal/engine"
	pe_tls "kube-policy-engine/internal/tls"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(policyv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var webhookPort int
	var certDir string
	var failOpen bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9090", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.IntVar(&webhookPort, "port", 8443, "The port the webhook server binds to.")
	flag.StringVar(&certDir, "cert-dir", "/etc/tls", "Directory containing tls.crt and tls.key")
	flag.BoolVar(&failOpen, "fail-open", true, "Allow resource if policy evaluation fails")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable default controller-runtime metrics server, we use custom
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize Engine and Registry
	eng := engine.NewEngine()
	registry := engine.NewRegistry(eng)

	// Setup Policy Controller
	if err = (&controller.PolicyReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Registry: registry,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Policy")
		os.Exit(1)
	}

	// Setup Healthz
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Setup Custom Metrics Server
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		setupLog.Info("starting metrics server", "addr", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, nil); err != nil {
			setupLog.Error(err, "metrics server failed")
			os.Exit(1)
		}
	}()

	// Setup Webhook Server
	h := &admission.Handler{
		Registry: registry,
		Engine:   eng,
		FailOpen: failOpen,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/validate", h.ServeValidate)
	mux.HandleFunc("/mutate", h.ServeMutate)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	reloader, err := pe_tls.NewReloader(certDir+"/tls.crt", certDir+"/tls.key")
	if err != nil {
		setupLog.Error(err, "failed to initialize TLS reloader")
		// Continue without TLS reloader if certs are missing immediately
	} else {
		stopCh := make(chan struct{})
		defer close(stopCh)
		go func() {
			_ = reloader.Start(stopCh)
		}()
	}

	server := &http.Server{
		Addr:    ":8443",
		Handler: mux,
		TLSConfig: &tls.Config{
			GetCertificate: reloader.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		},
	}

	go func() {
		setupLog.Info("starting webhook server", "port", webhookPort)
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			setupLog.Error(err, "webhook server failed")
			os.Exit(1)
		}
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
