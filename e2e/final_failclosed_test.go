package e2e

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (h *finalSafetyHarness) managementKube(t *testing.T) kubernetes.Interface {
	t.Helper()
	if h.logs == nil {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		rules.ExplicitPath = h.cfg.Kubeconfig
		rest, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: h.cfg.Context}).ClientConfig()
		if err != nil {
			t.Fatal(err)
		}
		rest.Timeout = 15 * time.Second
		h.logs, err = kubernetes.NewForConfig(rest)
		if err != nil {
			t.Fatal(err)
		}
	}
	return h.logs
}

func (h *finalSafetyHarness) jobTrace(t *testing.T, job batchv1.Job) map[string]any {
	t.Helper()
	var pods corev1.PodList
	if err := h.kube.List(h.ctx, &pods, client.InNamespace(h.namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		t.Fatal(err)
	}
	logs := map[string]string{}
	for _, pod := range pods.Items {
		containers := append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...)
		for _, container := range containers {
			stream, err := h.managementKube(t).CoreV1().Pods(h.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container.Name}).Stream(h.ctx)
			if err != nil {
				logs[pod.Name+"/"+container.Name] = err.Error()
				continue
			}
			body, readErr := io.ReadAll(io.LimitReader(stream, 8<<20))
			_ = stream.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			logs[pod.Name+"/"+container.Name] = string(body)
		}
	}
	return map[string]any{"job": job, "pods": pods, "logs": logs}
}

func (h *finalSafetyHarness) wrongAccount(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "e-wrong-account", Namespace: h.namespace}, Data: map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("111111111111"), "AWS_SECRET_ACCESS_KEY": []byte("test")}}
	if err := h.kube.Create(h.ctx, secret); err != nil {
		t.Fatal(err)
	}
	stack := h.createStack(t, "e-account", func(s *platform.InfraStack) { s.Spec.ExecutionProfile.CredentialsSecretRef.Name = secret.Name })
	var diagnostic string
	h.await(t, "controller rejects dummy wrong-account credentials", func() (bool, error) {
		body, err := os.ReadFile(h.controllerLog)
		if err != nil {
			return false, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "e-account") && strings.Contains(line, "emulator credential account/identity mismatch") {
				diagnostic = line
				return true, nil
			}
		}
		return false, nil
	})
	time.Sleep(2 * time.Second)
	h.noApply(t, stack, "wrong-account-blocked")
	if len(h.runs(t, stack.Name)) != 0 {
		t.Fatal("wrong-account configuration created a TerraformRun")
	}
	identity, identityErr := sts.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("111111111111", "test", "")}, func(o *sts.Options) { o.BaseEndpoint = aws.String(h.cfg.HostEndpoint) }).GetCallerIdentity(h.ctx, &sts.GetCallerIdentityInput{})
	recordGateEvidence(t, h.namespace, "e-wrong-account-result", map[string]any{"stack": stack, "secretUID": secret.UID, "secretResourceVersion": secret.ResourceVersion, "controllerDiagnostic": diagnostic, "dummySTSIdentity": identity, "dummySTSError": fmt.Sprint(identityErr), "scope": "configured dummy account binding only; not production IAM isolation", "terraformRunCount": 0})
	h.removeUnapplied(t, stack)
}

func (h *finalSafetyHarness) gitOutage(t *testing.T) {
	parsed, err := url.Parse(h.cfg.SourceURL)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(parsed.Hostname(), ".")
	if len(parts) < 4 || parts[1] != h.cfg.FinalSafetyGitNamespace || h.cfg.FinalSafetyOwnerID == "" {
		t.Fatal("explicit owned fixed Git Service is required")
	}
	key := client.ObjectKey{Namespace: parts[1], Name: parts[0]}
	var service corev1.Service
	if err := h.kube.Get(h.ctx, key, &service); err != nil {
		t.Fatal(err)
	}
	if service.Labels["platform.example.io/qa-run"] != h.cfg.FinalSafetyOwnerID {
		t.Fatal("refusing outage on an unowned source Service")
	}
	original := service.DeepCopy()
	restored := false
	restore := func() {
		if restored {
			return
		}
		var live corev1.Service
		if err := h.kube.Get(h.ctx, key, &live); err != nil {
			h.clean = false
			t.Error(err)
			return
		}
		if live.UID != original.UID {
			h.clean = false
			t.Error("source Service identity changed; refusing to patch")
			return
		}
		before := live.DeepCopy()
		live.Spec.Selector = original.Spec.Selector
		if err := h.kube.Patch(h.ctx, &live, client.MergeFrom(before)); err != nil {
			h.clean = false
			t.Error(err)
			return
		}
		restored = true
	}
	defer restore()
	stack := h.createStack(t, "e-git", nil)
	goodPlan := h.savedPlan(t, stack, "Reconcile")
	h.removeUnapplied(t, stack)
	h.await(t, "old source consumer removed normally", func() (bool, error) { return len(h.runs(t, stack.Name)) == 0, nil })
	before := service.DeepCopy()
	service.Spec.Selector = map[string]string{"platform.example.io/qa-outage": "no-such-owned-source-pod"}
	if err := h.kube.Patch(h.ctx, &service, client.MergeFrom(before)); err != nil {
		t.Fatal(err)
	}
	h.await(t, "owned Git Service has no ready endpoints", func() (bool, error) {
		slices, err := h.managementKube(t).DiscoveryV1().EndpointSlices(key.Namespace).List(h.ctx, metav1.ListOptions{LabelSelector: "kubernetes.io/service-name=" + key.Name})
		if err != nil {
			return false, err
		}
		for _, slice := range slices.Items {
			for _, endpoint := range slice.Endpoints {
				if len(endpoint.Addresses) > 0 && (endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready) {
					return false, nil
				}
			}
		}
		return true, nil
	})
	stack = h.createStack(t, "e-git", func(s *platform.InfraStack) {
		s.Spec.Executor.ExecutionTimeout = metav1.Duration{Duration: 20 * time.Second}
	})
	h.approve(t, goodPlan, "e-git-old-approval")
	run, job := h.activePlan(t, stack)
	h.await(t, "Git transport outage fails the real clone Job", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(&job), &job); err != nil {
			return false, err
		}
		return job.Status.Failed > 0, nil
	})
	h.noApply(t, stack, "source-outage-blocked")
	recordGateEvidence(t, h.namespace, "e-git-outage-result", map[string]any{"healthyPlan": goodPlan, "outageRun": run, "sourceServiceUID": service.UID, "sourceSelectorBefore": original.Spec.Selector, "sourceSelectorDuring": service.Spec.Selector, "actualJob": h.jobTrace(t, job), "noOldSourceFallback": true})
	restore()
	h.removeUnapplied(t, stack)
	h.await(t, "failed source consumer removed normally", func() (bool, error) { return len(h.runs(t, stack.Name)) == 0, nil })
	stack = h.createStack(t, "e-git", nil)
	fresh := h.savedPlan(t, stack, "Reconcile")
	if fresh.UID == goodPlan.UID {
		t.Fatal("source recovery reused an old Plan UID")
	}
	h.noApply(t, stack, "source-recovery-still-unapproved")
	recordGateEvidence(t, h.namespace, "e-git-recovered", map[string]any{"freshPlan": fresh, "originalPlanUID": goodPlan.UID, "serviceUIDUnchanged": service.UID == original.UID})
	h.removeUnapplied(t, stack)
}

func (h *finalSafetyHarness) foreignJob(t *testing.T) {
	var baseline platform.TerraformRun
	for _, run := range h.runs(t, h.template.Name) {
		if run.Spec.Operation == "Plan" && run.Spec.PlanMode == "Reconcile" {
			baseline = run
			break
		}
	}
	if baseline.UID == "" {
		t.Fatal("real baseline Plan is required")
	}
	h.stop()
	paused := true
	defer func() {
		if paused {
			h.start()
		}
	}()
	run := &platform.TerraformRun{ObjectMeta: metav1.ObjectMeta{Name: "e-foreign-plan", Namespace: h.namespace}, Spec: *baseline.Spec.DeepCopy()}
	run.Spec.StackRef.Name = "e-foreign"
	if err := h.kube.Create(h.ctx, run); err != nil {
		t.Fatal(err)
	}
	h.remaining[run.Name] = true
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "e-foreign-owner", Namespace: h.namespace}, Data: map[string]string{"ownership": "foreign to TerraformRun; owned by this QA test"}}
	if err := h.kube.Create(h.ctx, owner); err != nil {
		t.Fatal(err)
	}
	controller := true
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: run.Name, Namespace: h.namespace, Labels: map[string]string{"platform.example.io/terraform-run-uid": string(run.UID)}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID, Controller: &controller}}}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: new(bool), Containers: []corev1.Container{{Name: "foreign", Image: "docker.io/library/busybox@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662", ImagePullPolicy: corev1.PullNever, Command: []string{"/bin/sh", "-c", "echo OWNED_FOREIGN_JOB_FIXTURE"}}}}}}}
	if err := h.kube.Create(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	h.await(t, "foreign fixture completes before reconstruction", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(job), job); err != nil {
			return false, err
		}
		return job.Status.Succeeded == 1, nil
	})
	originalJob := job.DeepCopy()
	h.start()
	paused = false
	h.await(t, "controller refuses a completed foreign Job", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(run), run); err != nil {
			return false, err
		}
		return run.Status.ExecutionOutcome == "Indeterminate", nil
	})
	if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(job), job); err != nil {
		t.Fatal(err)
	}
	if job.UID != originalJob.UID || hasOwnerUID(job.OwnerReferences, run.UID) || len(job.OwnerReferences) != 1 || job.OwnerReferences[0].UID != owner.UID {
		t.Fatal("controller adopted or replaced the foreign Job")
	}
	ghost := &platform.InfraStack{ObjectMeta: metav1.ObjectMeta{Name: "e-foreign", Namespace: h.namespace}, Spec: platform.InfraStackSpec{Variables: platform.VariablesSpec{Values: map[string]string{"bucket_name": h.namespace + "-e-foreign-cloud"}}}}
	h.noApply(t, ghost, "foreign-job-blocked")
	recordGateEvidence(t, h.namespace, "e-foreign-job-result", map[string]any{"run": run, "originalJob": originalJob, "afterJob": job, "foreignOwnerUID": owner.UID, "controllerAdopted": false})
	for _, object := range []client.Object{run, job, owner} {
		uid := object.GetUID()
		if err := h.kube.Delete(h.ctx, object, client.Preconditions{UID: &uid}); err != nil {
			t.Fatal(err)
		}
	}
	delete(h.remaining, run.Name)
}
