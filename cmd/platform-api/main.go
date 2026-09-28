package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/console"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		logger.Error("register core Kubernetes API", "error", err)
		os.Exit(1)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		logger.Error("register platform API", "error", err)
		os.Exit(1)
	}

	config := ctrl.GetConfigOrDie()
	kubeClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		logger.Error("create Kubernetes API client", "error", err)
		os.Exit(1)
	}
	provider, generator, err := console.NewDraftGeneratorFromEnvironment()
	if err != nil {
		logger.Error("configure local AI draft provider", "error", err)
		os.Exit(1)
	}
	artifactStore, err := console.NewArtifactStoreFromEnvironment()
	if err != nil {
		logger.Error("configure local artifact reader", "error", err)
		os.Exit(1)
	}
	api, err := console.NewServer(kubeClient, artifactStore, provider, generator, logger)
	if err != nil {
		logger.Error("configure platform API", "error", err)
		os.Exit(1)
	}

	address := strings.TrimSpace(os.Getenv("PCP_PLATFORM_API_ADDR"))
	if address == "" {
		address = "127.0.0.1:8090"
	}
	host, _, addressErr := net.SplitHostPort(address)
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if addressErr != nil || !(strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())) {
		logger.Error("platform API must bind to a loopback address", "address", address)
		os.Exit(1)
	}
	server := &http.Server{Addr: address, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("platform API listening", "address", address, "aiProvider", provider, "artifactPreview", artifactStore != nil)
		serveErr <- server.ListenAndServe()
	}()
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("platform API stopped", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			logger.Error("platform API shutdown failed", "error", err)
			os.Exit(1)
		}
	}
}
