package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

type controllerDeployment struct {
	kube            client.Client
	logs            kubernetes.Interface
	namespace, name string
	deployment      *appsv1.Deployment
}

// This lane creates only explicitly owned QA objects. It reuses the product
// ClusterRole rules and an externally supplied, private Pod-routable target
// kubeconfig. No shared Deployment, Role, engine or cluster configuration changes.
func startControllerDeployment(t *testing.T, ctx context.Context, kube client.Client, scheme *runtime.Scheme, management *rest.Config, cfg configuration, namespace, targetsFile string, complete *bool) *controllerDeployment {
	t.Helper()
	if !strings.Contains(cfg.ControllerImage, "@sha256:") || !filepath.IsAbs(cfg.ControllerKubeconfig) {
		t.Fatal("Deployment lane requires a pinned controller image and explicit private Pod target kubeconfig")
	}
	for _, add := range []func(*runtime.Scheme) error{appsv1.AddToScheme, coordinationv1.AddToScheme, rbacv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	name := namespace + "-controller"
	labels := map[string]string{"platform.example.io/e2e": namespace, "app.kubernetes.io/name": name}
	meta := func(n string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: n, Namespace: namespace, Labels: labels}
	}
	profile, err := os.ReadFile(targetsFile)
	if err != nil {
		t.Fatal(err)
	}
	kubeconfig, err := os.ReadFile(cfg.ControllerKubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	roleBytes, err := os.ReadFile(filepath.Join("..", "config", "rbac", "role.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(roleBytes, &role); err != nil {
		t.Fatal(err)
	}
	role.ObjectMeta = metav1.ObjectMeta{Name: name, Labels: labels}
	binding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: name, Namespace: namespace}}}
	var ownerNamespace corev1.Namespace
	if err := kube.Get(ctx, client.ObjectKey{Name: namespace}, &ownerNamespace); err != nil {
		t.Fatal(err)
	}
	owner := metav1.OwnerReference{APIVersion: "v1", Kind: "Namespace", Name: namespace, UID: ownerNamespace.UID}
	role.OwnerReferences = []metav1.OwnerReference{owner}
	binding.OwnerReferences = []metav1.OwnerReference{owner}
	for _, object := range []client.Object{
		&role, binding, &corev1.ServiceAccount{ObjectMeta: meta(name)},
		&corev1.Secret{ObjectMeta: meta(name + "-targets"), Data: map[string][]byte{"kubeconfig": kubeconfig, "targets.json": profile}},
	} {
		if err := kube.Create(ctx, object); err != nil {
			t.Fatal(err)
		}
		kind := strings.ReplaceAll(strings.TrimPrefix(fmt.Sprintf("%T", object), "*"), ".", "-")
		recordGateEvidence(t, namespace, "deployment-object-"+kind+"-"+object.GetName(), map[string]any{"type": kind, "name": object.GetName(), "namespace": object.GetNamespace(), "uid": object.GetUID(), "createdAt": object.GetCreationTimestamp(), "ownerNamespaceUID": ownerNamespace.UID})
	}
	manager := corev1.Container{Name: "manager", Image: cfg.ControllerImage, ImagePullPolicy: corev1.PullNever, Args: []string{"--leader-elect=true", "--metrics-bind-address=0", "--health-probe-bind-address=:8081"},
		Env:             []corev1.EnvVar{{Name: "PCP_KUBECONFIG", Value: "/targets/kubeconfig"}, {Name: "PCP_TARGETS_FILE", Value: "/targets/targets.json"}, {Name: "PCP_TARGET_CONTEXTS", Value: ""}, {Name: "PCP_ENABLE_AWS_EKS", Value: "false"}, {Name: "PCP_ENABLE_TERRAFORM_EXECUTION", Value: "true"}, {Name: "PCP_ARTIFACT_ENDPOINT", Value: cfg.PodEndpoint}, {Name: "PCP_ARTIFACT_REGION", Value: "us-east-1"}, {Name: "PCP_ARTIFACT_BUCKET", Value: namespace}, {Name: "PCP_LEADER_ELECTION_NAMESPACE", Value: namespace}, {Name: "PCP_MAX_CONCURRENT_APPLIES", Value: "1"}, {Name: "AWS_ACCESS_KEY_ID", Value: "000000000000"}, {Name: "AWS_SECRET_ACCESS_KEY", Value: "test"}, {Name: "AWS_SESSION_TOKEN", Value: ""}},
		VolumeMounts:    []corev1.VolumeMount{{Name: "targets", MountPath: "/targets", ReadOnly: true}},
		SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
	}
	manager.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt(8081)}}, InitialDelaySeconds: 1, PeriodSeconds: 2}
	manager.LivenessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(8081)}}, InitialDelaySeconds: 5, PeriodSeconds: 5}
	deployment := &appsv1.Deployment{ObjectMeta: meta(name), Spec: appsv1.DeploymentSpec{Replicas: ptr.To(int32(2)), Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{ServiceAccountName: name, SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To(int64(65532)), FSGroup: ptr.To(int64(65532)), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{manager}, Volumes: []corev1.Volume{{Name: "targets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name + "-targets", DefaultMode: ptr.To(int32(0440))}}}}}}}}
	if err := kube.Create(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	logs, err := kubernetes.NewForConfig(management)
	if err != nil {
		t.Fatal(err)
	}
	driver := &controllerDeployment{kube: kube, logs: logs, namespace: namespace, name: name, deployment: deployment}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		driver.captureLogs(t, cleanup, "final")
		var live appsv1.Deployment
		if err := kube.Get(cleanup, client.ObjectKeyFromObject(deployment), &live); err != nil {
			t.Error(err)
			return
		}
		if live.UID != deployment.UID {
			t.Error("owned Deployment identity changed; refusing cleanup")
			return
		}
		before := live.DeepCopy()
		live.Spec.Replicas = ptr.To(int32(0))
		if err := kube.Patch(cleanup, &live, client.MergeFrom(before)); err != nil {
			t.Error(err)
			return
		}
		if !*complete {
			t.Logf("retained owned stopped Deployment namespace=%s UID=%s", namespace, live.UID)
			return
		}
		for _, object := range []client.Object{binding, &role} {
			uid := object.GetUID()
			if err := kube.Delete(cleanup, object, client.Preconditions{UID: &uid}); err != nil {
				t.Error(err)
			}
		}
	})
	driver.awaitLeader(t, ctx, "initial", "")
	return driver
}

func (d *controllerDeployment) awaitLeader(t *testing.T, ctx context.Context, phase, previous string) coordinationv1.Lease {
	t.Helper()
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	for {
		var lease coordinationv1.Lease
		err := d.kube.Get(ctx, client.ObjectKey{Namespace: d.namespace, Name: "platform-control-plane.platform.example.io"}, &lease)
		var pods corev1.PodList
		if listErr := d.kube.List(ctx, &pods, client.InNamespace(d.namespace), client.MatchingLabels{"app.kubernetes.io/name": d.name}); listErr != nil {
			t.Fatal(listErr)
		}
		running, holders := 0, 0
		for _, pod := range pods.Items {
			if !d.ownsPod(ctx, &pod) {
				t.Fatalf("unexpected Pod shares the owned Deployment selector: %s", pod.Name)
			}
			if pod.DeletionTimestamp.IsZero() && len(pod.Status.ContainerStatuses) == 1 && pod.Status.ContainerStatuses[0].State.Running != nil {
				running++
				if lease.Spec.HolderIdentity != nil && strings.HasPrefix(*lease.Spec.HolderIdentity, pod.Name+"_") {
					holders++
				}
			}
		}
		var deployment appsv1.Deployment
		if deploymentErr := d.kube.Get(ctx, client.ObjectKeyFromObject(d.deployment), &deployment); deploymentErr != nil {
			t.Fatal(deploymentErr)
		}
		if err == nil && running == 2 && deployment.Status.ReadyReplicas == 2 && holders == 1 && lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != previous && lease.Spec.RenewTime != nil && time.Since(lease.Spec.RenewTime.Time) < 20*time.Second {
			if err := d.kube.Get(ctx, client.ObjectKeyFromObject(d.deployment), &deployment); err != nil {
				t.Fatal(err)
			}
			recordGateEvidence(t, d.namespace, "leader-"+phase, map[string]any{"deployment": deployment, "pods": pods, "lease": lease, "runningReplicas": running, "currentLeaseHolders": holders})
			t.Logf("Deployment failover phase=%s deploymentUID=%s running=%d ready=%d leaseUID=%s rv=%s holder=%s", phase, deployment.UID, running, deployment.Status.ReadyReplicas, lease.UID, lease.ResourceVersion, *lease.Spec.HolderIdentity)
			return lease
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			recordGateEvidence(t, d.namespace, "leader-failed-"+phase, map[string]any{"deployment": deployment, "pods": pods, "lease": lease, "runningReplicas": running, "currentLeaseHolders": holders})
			d.captureLogs(t, ctx, "failed-"+phase)
			t.Fatalf("two Ready replicas / fresh single leader not observed phase=%s running=%d ready=%d", phase, running, deployment.Status.ReadyReplicas)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (d *controllerDeployment) failover(t *testing.T, ctx context.Context, phase string) {
	t.Helper()
	before := d.awaitLeader(t, ctx, "before-"+phase, "")
	d.captureLogs(t, ctx, "before-"+phase)
	var pods corev1.PodList
	if err := d.kube.List(ctx, &pods, client.InNamespace(d.namespace), client.MatchingLabels{"app.kubernetes.io/name": d.name}); err != nil {
		t.Fatal(err)
	}
	deleted := false
	for _, pod := range pods.Items {
		if !d.ownsPod(ctx, &pod) {
			t.Fatal("refusing to exit a Pod without the owned Deployment UID chain")
		}
		if strings.HasPrefix(*before.Spec.HolderIdentity, pod.Name+"_") {
			uid := pod.UID
			if err := d.kube.Delete(ctx, &pod, client.Preconditions{UID: &uid}, client.GracePeriodSeconds(0)); err != nil {
				t.Fatal(err)
			}
			deleted = true
			t.Logf("owned leader Pod exited phase=%s pod=%s uid=%s", phase, pod.Name, pod.UID)
			break
		}
	}
	if !deleted {
		t.Fatal("Lease holder not found among owned controller replicas")
	}
	after := d.awaitLeader(t, ctx, "after-"+phase, *before.Spec.HolderIdentity)
	if after.UID != before.UID || after.ResourceVersion == before.ResourceVersion {
		t.Fatal("leader Lease identity/history was lost")
	}
}

func (d *controllerDeployment) captureLogs(t *testing.T, ctx context.Context, phase string) {
	t.Helper()
	dir := os.Getenv("KPCP_E2E_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	var pods corev1.PodList
	if err := d.kube.List(ctx, &pods, client.InNamespace(d.namespace), client.MatchingLabels{"app.kubernetes.io/name": d.name}); err != nil {
		t.Log(err)
		return
	}
	for _, pod := range pods.Items {
		if !d.ownsPod(ctx, &pod) {
			continue
		}
		data, err := d.logs.CoreV1().Pods(d.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "manager", TailLines: ptr.To(int64(4000))}).DoRaw(ctx)
		if err != nil {
			t.Logf("capture owned Pod log %s: %v", pod.Name, err)
			continue
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Error(err)
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%s-%s.log", d.namespace, phase, pod.Name)), data, 0600); err != nil {
			t.Error(err)
		}
	}
}

func (d *controllerDeployment) ownsPod(ctx context.Context, pod *corev1.Pod) bool {
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.Kind != "ReplicaSet" {
		return false
	}
	var replica appsv1.ReplicaSet
	if err := d.kube.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: owner.Name}, &replica); err != nil {
		return false
	}
	return replica.UID == owner.UID && hasOwnerUID(replica.OwnerReferences, d.deployment.UID)
}
