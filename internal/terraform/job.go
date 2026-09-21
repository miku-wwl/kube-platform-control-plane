package terraform

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SourceGitCommit      = "GitCommit"
	SourceRetainedBundle = "RetainedBundle"
	DefaultGitImage      = "alpine/git:2.45.2"
)

type JobRequest struct {
	Name                        string
	Namespace                   string
	RunUID                      string
	RunnerImage                 string
	SourceType                  string
	SourceURL                   string
	SourceRevision              string
	SourcePath                  string
	SourceRoot                  string
	SourceBundleRef             string
	SourceBundleDigest          string
	Operation                   string
	Destroy                     bool
	WorkingDir                  string
	Workspace                   string
	BackendConfigMap            string
	BackendConfigPath           string
	PlanPath                    string
	LockTimeout                 time.Duration
	ExecutionTimeout            time.Duration
	Parallelism                 *int32
	GitImage                    string
	ArtifactEndpoint            string
	ArtifactRegion              string
	ArtifactBucket              string
	ArtifactPrefix              string
	PlanRef                     string
	PlanDigest                  string
	TargetDiscoveryRef          string
	TargetDiscoveryDigest       string
	BackendConfigArtifactRef    string
	BackendConfigArtifactDigest string
	VariableSecretRefs          []corev1.LocalObjectReference
	VariableSecretVariables     []VariableSecretReference
	ServiceAccountName          string
	ExpectedTerraformVersion    string
}

type VariableSecretReference struct {
	Variable string
	Name     string
	Key      string
}

func BuildJob(request JobRequest) (*batchv1.Job, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	deadline := int64(math.Ceil(request.ExecutionTimeout.Seconds()))
	labels := map[string]string{
		"platform.example.io/terraform-run":     request.Name,
		"platform.example.io/terraform-run-uid": request.RunUID,
	}
	workspaceVolume := corev1.Volume{
		Name:         "workspace",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
	tmpVolume := corev1.Volume{
		Name:         "tmp",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
	volumes := []corev1.Volume{workspaceVolume, tmpVolume}
	volumeMount := corev1.VolumeMount{Name: "workspace", MountPath: "/workspace"}
	tmpMount := corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"}
	if request.BackendConfigMap != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "backend-config",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: request.BackendConfigMap},
			}},
		})
	}

	initContainers := []corev1.Container{}
	if request.SourceType == SourceGitCommit {
		gitUser := int64(1000)
		gitGroup := int64(1000)
		gitSecurityContext := &corev1.SecurityContext{RunAsUser: &gitUser, RunAsGroup: &gitGroup, RunAsNonRoot: pointerBool(true), AllowPrivilegeEscalation: pointerBool(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
		initContainers = []corev1.Container{
			{
				Name:            "source-clone",
				Image:           request.GitImage,
				Command:         []string{"git"},
				Args:            []string{"clone", "--no-checkout", request.SourceURL, "/workspace/terraform"},
				VolumeMounts:    []corev1.VolumeMount{volumeMount},
				SecurityContext: gitSecurityContext.DeepCopy(),
			},
			{
				Name:            "source-checkout",
				Image:           request.GitImage,
				Command:         []string{"git"},
				Args:            []string{"-C", "/workspace/terraform", "checkout", "--detach", request.SourceRevision},
				VolumeMounts:    []corev1.VolumeMount{volumeMount},
				SecurityContext: gitSecurityContext.DeepCopy(),
			},
		}
	}

	args := []string{
		"--operation=" + request.Operation,
		"--run-uid=" + request.RunUID,
		"--expected-terraform-version=" + request.ExpectedTerraformVersion,
		"--working-dir=" + request.WorkingDir,
		"--workspace=" + request.Workspace,
		"--plan=" + request.PlanPath,
		"--lock-timeout=" + request.LockTimeout.String(),
	}
	if request.Destroy {
		args = append(args, "--destroy=true")
	}
	if request.BackendConfigMap != "" {
		args = append(args, "--backend-config="+request.BackendConfigPath)
	}
	if request.Parallelism != nil {
		args = append(args, fmt.Sprintf("--parallelism=%d", *request.Parallelism))
	}
	if request.ArtifactBucket != "" {
		if request.ArtifactEndpoint != "" {
			args = append(args, "--artifact-endpoint="+request.ArtifactEndpoint)
		}
		args = append(args, "--artifact-region="+request.ArtifactRegion, "--artifact-bucket="+request.ArtifactBucket)
		if request.ArtifactPrefix != "" {
			args = append(args, "--artifact-prefix="+request.ArtifactPrefix)
		}
	}
	if request.SourceBundleRef != "" {
		args = append(args,
			"--source-root="+sourceRootForJob(request),
			"--source-bundle-ref="+request.SourceBundleRef,
			"--source-bundle-digest="+request.SourceBundleDigest,
		)
	} else if request.SourceRoot != "" {
		args = append(args, "--source-root="+request.SourceRoot)
	}
	if request.PlanRef != "" {
		args = append(args, "--plan-ref="+request.PlanRef, "--plan-digest="+request.PlanDigest)
	}
	if request.TargetDiscoveryRef != "" {
		args = append(args, "--target-discovery-ref="+request.TargetDiscoveryRef, "--target-discovery-digest="+request.TargetDiscoveryDigest)
	}
	if request.BackendConfigArtifactRef != "" {
		args = append(args,
			"--backend-config="+request.BackendConfigPath,
			"--backend-config-ref="+request.BackendConfigArtifactRef,
			"--backend-config-digest="+request.BackendConfigArtifactDigest,
		)
	}

	runner := corev1.Container{
		Name:         "terraform-runner",
		Image:        request.RunnerImage,
		Command:      []string{"terraform-runner"},
		Args:         args,
		VolumeMounts: []corev1.VolumeMount{volumeMount},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                pointerInt64(1000),
			RunAsGroup:               pointerInt64(1000),
			RunAsNonRoot:             pointerBool(true),
			AllowPrivilegeEscalation: pointerBool(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			ReadOnlyRootFilesystem:   pointerBool(true),
		},
		Env: []corev1.EnvVar{{Name: "HOME", Value: "/workspace"}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")},
		},
	}
	runner.Env = append(runner.Env,
		corev1.EnvVar{Name: "PCP_POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
		corev1.EnvVar{Name: "PCP_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}},
	)
	runner.VolumeMounts = append(runner.VolumeMounts, tmpMount)
	if request.BackendConfigMap != "" {
		runner.VolumeMounts = append(runner.VolumeMounts, corev1.VolumeMount{Name: "backend-config", MountPath: "/workspace/backend", ReadOnly: true})
	}
	for _, reference := range request.VariableSecretVariables {
		if reference.Variable == "" || reference.Name == "" || reference.Key == "" {
			return nil, fmt.Errorf("secret variable, Secret name, and key are required")
		}
		runner.Env = append(runner.Env, corev1.EnvVar{
			Name: "TF_VAR_" + reference.Variable,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: reference.Name}, Key: reference.Key,
			}},
		})
	}
	if isLocalStackEndpoint(request.ArtifactEndpoint) {
		// LocalStack deliberately uses non-secret test credentials. Keep this
		// branch constrained to local endpoints so production Jobs never receive
		// synthetic AWS credentials.
		runner.Env = append(runner.Env,
			corev1.EnvVar{Name: "AWS_ACCESS_KEY_ID", Value: "test"},
			corev1.EnvVar{Name: "AWS_SECRET_ACCESS_KEY", Value: "test"},
			corev1.EnvVar{Name: "AWS_DEFAULT_REGION", Value: request.ArtifactRegion},
		)
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: request.Name, Namespace: request.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:          pointer(int32(0)),
			ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: pointerBool(true),
					ServiceAccountName:           request.ServiceAccountName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   pointerBool(true),
						FSGroup:        pointerInt64(1000),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					InitContainers: initContainers,
					Containers:     []corev1.Container{runner},
					Volumes:        volumes,
				},
			},
		},
	}, nil
}

func sourceRootForJob(request JobRequest) string {
	if request.SourceRoot != "" {
		return request.SourceRoot
	}
	return "/workspace/terraform"
}

func isLocalStackEndpoint(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "host.docker.internal", "localstack":
		return true
	default:
		return false
	}
}

func (r JobRequest) validate() error {
	if r.Name == "" || r.Namespace == "" || r.RunUID == "" {
		return fmt.Errorf("job name, namespace, and run UID are required")
	}
	if r.RunnerImage == "" {
		return fmt.Errorf("runner image is required")
	}
	if r.SourceType == SourceGitCommit && r.GitImage == "" {
		return fmt.Errorf("git image is required for GitCommit source")
	}
	if r.SourceType == SourceGitCommit {
		if r.SourceURL == "" || r.SourceRevision == "" {
			return fmt.Errorf("GitCommit source URL and revision are required")
		}
	} else if r.SourceType == SourceRetainedBundle {
		if r.SourceBundleRef == "" || r.SourceBundleDigest == "" {
			return fmt.Errorf("RetainedBundle source ref and digest are required")
		}
	} else {
		return fmt.Errorf("source type %q is not supported by the Phase 2 runner", r.SourceType)
	}
	if r.Operation != "Plan" && r.Operation != "Apply" {
		return fmt.Errorf("operation must be Plan or Apply")
	}
	if r.Operation == "Apply" && r.SourceType != SourceRetainedBundle {
		return fmt.Errorf("Apply requires a RetainedBundle source")
	}
	if r.Destroy && r.SourceType != SourceRetainedBundle {
		return fmt.Errorf("Destroy requires a RetainedBundle source")
	}
	if r.Operation == "Apply" && r.PlanRef == "" {
		return fmt.Errorf("Apply requires an immutable saved plan ref")
	}
	if r.WorkingDir == "" || r.Workspace == "" || r.PlanPath == "" {
		return fmt.Errorf("working directory, workspace, and plan path are required")
	}
	if r.ExecutionTimeout <= 0 || r.LockTimeout < 0 {
		return fmt.Errorf("execution timeout must be positive and lock timeout cannot be negative")
	}
	if r.BackendConfigMap != "" && r.BackendConfigPath == "" {
		return fmt.Errorf("backend config path is required when a backend ConfigMap is mounted")
	}
	if r.ArtifactBucket != "" && r.ArtifactRegion == "" {
		return fmt.Errorf("artifact region is required with artifact bucket")
	}
	if r.ArtifactPrefix != "" && r.ArtifactBucket == "" {
		return fmt.Errorf("artifact bucket is required with artifact prefix")
	}
	if r.SourceBundleRef != "" && (r.ArtifactRegion == "" || r.ArtifactBucket == "") {
		return fmt.Errorf("artifact region and bucket are required for retained source bundle")
	}
	if r.PlanRef != "" && r.PlanDigest == "" {
		return fmt.Errorf("plan digest is required with plan ref")
	}
	if r.TargetDiscoveryRef != "" && r.TargetDiscoveryDigest == "" {
		return fmt.Errorf("target discovery digest is required with target discovery ref")
	}
	if r.BackendConfigArtifactRef != "" && r.BackendConfigArtifactDigest == "" {
		return fmt.Errorf("backend artifact digest is required with backend artifact ref")
	}
	if r.SourcePath != "" && path.IsAbs(r.SourcePath) {
		return fmt.Errorf("source path must be relative")
	}
	for _, reference := range r.VariableSecretVariables {
		if reference.Variable == "" || reference.Name == "" || reference.Key == "" {
			return fmt.Errorf("secret variable, Secret name, and key are required")
		}
	}
	return nil
}

func pointer(value int32) *int32 {
	return &value
}

func pointerBool(value bool) *bool {
	return &value
}

func pointerInt64(value int64) *int64 {
	return &value
}
