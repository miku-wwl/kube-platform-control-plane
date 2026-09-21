package main

import (
	"context"
	"flag"
	"os"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/controller"
	"github.com/miku-wwl/kube-platform-control-plane/internal/observability"
	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
	targetresolver "github.com/miku-wwl/kube-platform-control-plane/internal/target"
)

var scheme = runtime.NewScheme()

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = platformv1alpha1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election for controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: false})))
	setupLog := ctrl.Log.WithName("setup")

	clusterConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(clusterConfig, ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "platform-control-plane.platform.example.io",
		LeaderElectionNamespace: envOrDefault("PCP_LEADER_ELECTION_NAMESPACE", "platform-system"),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		setupLog.Error(err, "unable to create Kubernetes client for terminal evidence capture")
		os.Exit(1)
	}
	targetClient, targetResolver, targetVerifier, err := buildTargetClient()
	if err != nil {
		setupLog.Error(err, "unable to create target Kubernetes client")
		os.Exit(1)
	}
	planLimit := positiveEnvInt("PCP_MAX_CONCURRENT_PLANS", 8)
	applyLimit := positiveEnvInt("PCP_MAX_CONCURRENT_APPLIES", 4)
	executionGate := reliability.NewGate(planLimit, applyLimit)
	if result, err := controller.ReconstructActiveTerraformRuns(context.Background(), mgr.GetAPIReader(), kubeClient, executionGate); err != nil {
		setupLog.Error(err, "unable to reconstruct active Terraform executions")
		os.Exit(1)
	} else {
		setupLog.Info("reconstructed active Terraform executions", "active", len(result.Active), "overCapacity", result.OverCapacity, "safetyViolation", result.SafetyViolation, "duration", result.Duration)
		observability.SetReconstructionState(result.OverCapacity, result.SafetyViolation)
	}

	if err := (&controller.PlatformEnvironmentReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create PlatformEnvironment controller")
		os.Exit(1)
	}
	if err := (&controller.EnvironmentClassReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create EnvironmentClass controller")
		os.Exit(1)
	}
	if err := (&controller.InfraStackReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), ArtifactEndpoint: os.Getenv("PCP_ARTIFACT_ENDPOINT"), ArtifactRegion: os.Getenv("PCP_ARTIFACT_REGION"), ArtifactBucket: os.Getenv("PCP_ARTIFACT_BUCKET"), TargetVerifier: targetVerifier}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create InfraStack controller")
		os.Exit(1)
	}
	if err := (&controller.TerraformRunReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ExecutionEnabled: strings.EqualFold(os.Getenv("PCP_ENABLE_TERRAFORM_EXECUTION"), "true"),
		ArtifactEndpoint: os.Getenv("PCP_ARTIFACT_ENDPOINT"),
		ArtifactRegion:   os.Getenv("PCP_ARTIFACT_REGION"),
		ArtifactBucket:   os.Getenv("PCP_ARTIFACT_BUCKET"),
		KubeClient:       kubeClient,
		Gate:             executionGate,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create TerraformRun controller")
		os.Exit(1)
	}
	if err := (&controller.ResourceSetReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), TargetClient: targetClient, TargetResolver: targetResolver}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create ResourceSet controller")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("execution-reconstruction", executionGate.ReadyCheck); err != nil {
		setupLog.Error(err, "unable to set up execution reconstruction ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "leaderElection", enableLeaderElection)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func buildTargetClient() (dynamic.Interface, targetresolver.ClientResolver, targetresolver.TargetVerifier, error) {
	contextNames := splitNonEmpty(os.Getenv("PCP_TARGET_CONTEXTS"))
	if len(contextNames) == 0 {
		contextNames = splitNonEmpty(os.Getenv("PCP_TARGET_CONTEXT"))
	}
	var factory *targetresolver.TargetClientFactory
	var kindResolver targetresolver.ClientResolver
	var first dynamic.Interface
	if len(contextNames) > 0 {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		if kubeconfig := os.Getenv("PCP_KUBECONFIG"); kubeconfig != "" {
			loadingRules.ExplicitPath = kubeconfig
		}
		factory = targetresolver.NewTargetClientFactory()
		for _, contextName := range contextNames {
			targetConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				loadingRules,
				&clientcmd.ConfigOverrides{CurrentContext: contextName},
			).ClientConfig()
			if err != nil {
				return nil, nil, nil, err
			}
			client, err := dynamic.NewForConfig(targetConfig)
			if err != nil {
				return nil, nil, nil, err
			}
			identity := targetresolver.RuntimeTargetIdentity{Provider: targetresolver.ProviderKind, AccountID: "local", Region: "local", ClusterName: contextName, IncarnationID: contextName}
			profile := targetresolver.TargetConnectionProfile{Endpoint: "kubeconfig:" + contextName, AuthMode: targetresolver.AuthKindContext, KubeContext: contextName, NetworkRouteProfile: "local-kind"}
			if err := factory.Register(identity, profile, client); err != nil {
				return nil, nil, nil, err
			}
			if first == nil {
				first = client
			}
		}
		kindResolver = factory
	}
	var awsResolver targetresolver.ClientResolver
	var targetVerifier targetresolver.TargetVerifier
	if strings.EqualFold(os.Getenv("PCP_ENABLE_AWS_EKS"), "true") {
		awsResolver = targetresolver.EKSTargetResolver{TokenProvider: targetresolver.NewAWSSTSTokenProvider()}
		targetVerifier = targetresolver.NewAWSEKSVerifier()
	}
	if kindResolver == nil && awsResolver == nil {
		return nil, nil, nil, nil
	}
	return first, targetresolver.ProviderClientResolver{Kind: kindResolver, AWS: awsResolver}, targetVerifier, nil
}

func splitNonEmpty(value string) []string {
	values := strings.Split(value, ",")
	result := make([]string, 0, len(values))
	for _, item := range values {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func positiveEnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
