package e2e

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Mutation faults use the full Environment/Class/Stack chain so both business
// finalizers, not just a standalone test Stack, must retain uncertain execution.
func (h *finalSafetyHarness) mutationEnvironment(t *testing.T, name string, partial bool) (*platform.InfraStack, *platform.PlatformEnvironment) {
	t.Helper()
	var base platform.EnvironmentClass
	if err := h.kube.Get(h.ctx, client.ObjectKey{Name: h.namespace}, &base); err != nil {
		t.Fatal(err)
	}
	class := base.DeepCopy()
	class.ObjectMeta = metav1.ObjectMeta{Name: h.namespace + "-" + name, Labels: map[string]string{"platform.example.io/e2e": h.namespace}}
	class.Status = platform.EnvironmentClassStatus{}
	class.Spec.Inputs["bucket_name"] = h.namespace + "-" + name + "-cloud"
	class.Spec.RuntimeProfile.RuntimeObjects = nil
	if partial {
		if len(h.cfg.FinalSafetyPartialRevision) != 40 {
			t.Fatal("pinned partial fixture required")
		}
		class.Spec.Source.Revision = h.cfg.FinalSafetyPartialRevision
	}
	if err := h.kube.Create(h.ctx, class); err != nil {
		t.Fatal(err)
	}
	h.await(t, name+" immutable Class Ready", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(class), class); err != nil {
			return false, err
		}
		return meta.IsStatusConditionTrue(class.Status.Conditions, "Ready"), nil
	})
	environment := &platform.PlatformEnvironment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.namespace}, Spec: platform.PlatformEnvironmentSpec{ClassRef: &corev1.LocalObjectReference{Name: class.Name}, Capacity: platform.CapacitySpec{NodeCount: 1}}}
	if err := h.kube.Create(h.ctx, environment); err != nil {
		t.Fatal(err)
	}
	stack := &platform.InfraStack{ObjectMeta: metav1.ObjectMeta{Name: name + "-infra", Namespace: h.namespace}}
	h.remaining[stack.Name] = true
	if h.environments == nil {
		h.environments = map[string]*platform.PlatformEnvironment{}
	}
	h.environments[stack.Name] = environment
	h.await(t, name+" real owned Stack", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(stack), stack); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return stack.Spec.OwnerEnvironmentUID == string(environment.UID) && len(stack.Finalizers) > 0, nil
	})
	recordGateEvidence(t, h.namespace, name+"-owned-chain", map[string]any{"class": class, "environment": environment, "stack": stack})
	return stack, environment
}

func (h *finalSafetyHarness) cloudState(t *testing.T, stack *platform.InfraStack, partial bool) map[string]bool {
	t.Helper()
	bucket := stack.Spec.Variables.Values["bucket_name"]
	if bucket == "" {
		t.Fatal("explicit owned bucket input required")
	}
	result := map[string]bool{}
	names := []string{bucket}
	if partial {
		names = append(names, bucket+"-second")
	}
	for _, name := range names {
		_, err := h.storage.HeadBucket(h.ctx, &s3.HeadBucketInput{Bucket: aws.String(name)})
		if err != nil && !artifacts.IsNotFound(err) {
			t.Fatal(err)
		}
		result[name] = err == nil
	}
	return result
}

func (h *finalSafetyHarness) activeApply(t *testing.T, stack *platform.InfraStack, plan platform.TerraformRun) (platform.TerraformRun, batchv1.Job) {
	t.Helper()
	approval := h.approve(t, plan, stack.Name+"-"+strings.ToLower(plan.Spec.PlanMode)+"-approval")
	var apply platform.TerraformRun
	var job batchv1.Job
	h.await(t, stack.Name+" exact approved "+plan.Spec.PlanMode+" Apply Job", func() (bool, error) {
		for _, run := range h.runs(t, stack.Name) {
			if run.Spec.Operation != "Apply" || run.Spec.PlanMode != plan.Spec.PlanMode {
				continue
			}
			if run.Spec.PlanRunUID != string(plan.UID) {
				return false, fmt.Errorf("Apply changed its Plan UID")
			}
			if run.Status.ExecutionOutcome == "Rejected" {
				return false, fmt.Errorf("approved fixture rejected: %+v", run.Status.Conditions)
			}
			if run.Status.JobRef == nil || run.Status.JobUID == "" {
				continue
			}
			if err := h.kube.Get(h.ctx, client.ObjectKey{Namespace: h.namespace, Name: run.Status.JobRef.Name}, &job); err != nil {
				return false, client.IgnoreNotFound(err)
			}
			if string(job.UID) != run.Status.JobUID || !hasOwnerUID(job.OwnerReferences, run.UID) {
				return false, fmt.Errorf("Apply Job identity mismatch")
			}
			if job.Status.Failed > 0 || job.Status.Succeeded > 0 {
				return false, fmt.Errorf("fixture finished before fault injection")
			}
			apply = run
			return true, nil
		}
		return false, nil
	})
	recordGateEvidence(t, h.namespace, stack.Name+"-"+strings.ToLower(plan.Spec.PlanMode)+"-active", map[string]any{"plan": plan, "approval": approval, "apply": apply, "job": job})
	return apply, job
}

func (h *finalSafetyHarness) appliedReady(t *testing.T, stack *platform.InfraStack, environment *platform.PlatformEnvironment, partial bool) {
	t.Helper()
	for name, exists := range h.cloudState(t, stack, partial) {
		if exists {
			t.Fatal("refusing fixture over a pre-existing bucket: ", name)
		}
	}
	plan := h.savedPlan(t, stack, "Reconcile")
	h.activeApply(t, stack, plan)
	h.await(t, environment.Name+" true Environment Ready", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(environment), environment); err != nil {
			return false, err
		}
		return meta.IsStatusConditionTrue(environment.Status.Conditions, "Ready"), nil
	})
	for name, exists := range h.cloudState(t, stack, partial) {
		if !exists {
			t.Fatal("Ready did not create actual QA resource: ", name)
		}
	}
}

func (h *finalSafetyHarness) applyTerminal(t *testing.T, apply *platform.TerraformRun, job *batchv1.Job) map[string]any {
	t.Helper()
	h.await(t, "actual Apply terminal classification", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(apply), apply); err != nil {
			return false, err
		}
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(job), job); err != nil {
			return false, err
		}
		if job.Status.Failed == 0 && job.Status.Succeeded == 0 {
			return false, nil
		}
		switch apply.Status.ExecutionOutcome {
		case "Succeeded", "Failed", "Indeterminate", "Rejected":
			return true, nil
		}
		return false, nil
	})
	trace := h.jobTrace(t, *job)
	recordGateEvidence(t, h.namespace, apply.Name+"-actual-job-trace", trace)
	for _, log := range trace["logs"].(map[string]string) {
		for _, line := range strings.Split(log, "\n") {
			var terminal map[string]any
			if json.Unmarshal([]byte(line), &terminal) == nil && terminal["terraformRunUID"] == string(apply.UID) && terminal["jobUID"] == string(job.UID) {
				return terminal
			}
		}
	}
	t.Fatal("exact Run/Job structured runner terminal not found in genuine Pod logs")
	return nil
}

func (h *finalSafetyHarness) noRepeatedApply(t *testing.T, stack *platform.InfraStack, apply platform.TerraformRun, job batchv1.Job) {
	t.Helper()
	time.Sleep(2 * time.Second)
	count := 0
	for _, run := range h.runs(t, stack.Name) {
		if run.Spec.Operation == "Apply" && run.Spec.PlanMode == apply.Spec.PlanMode {
			count++
			if run.UID != apply.UID || run.Status.JobUID != string(job.UID) {
				t.Fatal("uncertain mutation replaced Run/Job identity")
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one %s Apply, got %d", apply.Spec.PlanMode, count)
	}
	var live batchv1.Job
	if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(&job), &live); err != nil {
		t.Fatal(err)
	}
	if live.UID != job.UID {
		t.Fatal("uncertain mutation replaced its Job")
	}
}

func (h *finalSafetyHarness) restartObserved(t *testing.T, phase string) {
	t.Helper()
	leases := h.managementKube(t).CoordinationV1().Leases(h.namespace)
	before, err := leases.Get(h.ctx, "platform-control-plane.platform.example.io", metav1.GetOptions{})
	if err != nil || before.Spec.HolderIdentity == nil {
		t.Fatal("owned controller leader missing: ", err)
	}
	h.stop()
	h.start()
	h.await(t, "new owned controller has the same Lease UID", func() (bool, error) {
		after, err := leases.Get(h.ctx, before.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if after.UID != before.UID {
			return false, fmt.Errorf("restart replaced Lease UID")
		}
		ready := after.Spec.HolderIdentity != nil && *after.Spec.HolderIdentity != *before.Spec.HolderIdentity && after.Spec.RenewTime != nil && time.Since(after.Spec.RenewTime.Time) < 20*time.Second
		if ready {
			recordGateEvidence(t, h.namespace, phase+"-controller-restart", map[string]any{"before": before, "after": after})
		}
		return ready, nil
	})
}

func (h *finalSafetyHarness) retainFinalizers(t *testing.T, stack *platform.InfraStack, environment *platform.PlatformEnvironment, phase string) {
	t.Helper()
	if err := h.kube.Delete(h.ctx, environment, client.Preconditions{UID: &environment.UID}); err != nil {
		t.Fatal(err)
	}
	h.await(t, "uncertain Apply retains both business finalizers", func() (bool, error) {
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(stack), stack); err != nil {
			return false, err
		}
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(environment), environment); err != nil {
			return false, err
		}
		condition := meta.FindStatusCondition(stack.Status.Conditions, "Ready")
		return !environment.DeletionTimestamp.IsZero() && len(environment.Finalizers) > 0 && len(stack.Finalizers) > 0 && condition != nil && condition.Reason == "RecoveryRequired", nil
	})
	recordGateEvidence(t, h.namespace, phase+"-retained", map[string]any{"environment": environment, "stack": stack, "normalDeleteAttempted": true, "manualFinalizerPatch": false, "forcedDelete": false})
}

func (h *finalSafetyHarness) cloudFault(t *testing.T, method, bucket string) {
	t.Helper()
	h.setFault(t, []map[string]any{{"method": method, "path": "^" + regexp.QuoteMeta("/"+bucket) + "/?$", "action": "status", "status": 403}})
}

func (h *finalSafetyHarness) partialDestroy(t *testing.T) {
	stack, environment := h.mutationEnvironment(t, "f-partial", true)
	h.appliedReady(t, stack, environment, true)
	before := h.cloudState(t, stack, true)
	if err := h.kube.Delete(h.ctx, environment, client.Preconditions{UID: &environment.UID}); err != nil {
		t.Fatal(err)
	}
	plan := h.savedPlan(t, stack, "Destroy")
	apply, job := h.activeApply(t, stack, plan)
	bucket := stack.Spec.Variables.Values["bucket_name"]
	h.cloudFault(t, "DELETE", bucket)
	defer h.clearFault(t)
	terminal := h.applyTerminal(t, &apply, &job)
	after := h.cloudState(t, stack, true)
	if apply.Spec.PlanMode != "Destroy" || terminal["operation"] != "Apply" || terminal["terraformExitCode"] == float64(0) || terminal["mutationMayHaveOccurred"] != true || !after[bucket] || after[bucket+"-second"] {
		t.Fatalf("not a real partial Destroy Apply failure: terminal=%v cloud=%v", terminal, after)
	}
	h.clearFault(t)
	h.retainFinalizers(t, stack, environment, "f01-partial-destroy")
	h.noRepeatedApply(t, stack, apply, job)
	recordGateEvidence(t, h.namespace, "f01-partial-destroy-result", map[string]any{"plan": plan, "apply": apply, "jobUID": job.UID, "terminal": terminal, "cloudBefore": before, "cloudAfter": after, "destroyApplyCount": 1})
}

func (h *finalSafetyHarness) missingDestroy(t *testing.T) {
	stack, environment := h.mutationEnvironment(t, "f-terminal", false)
	h.appliedReady(t, stack, environment, false)
	before := h.cloudState(t, stack, false)
	if err := h.kube.Delete(h.ctx, environment, client.Preconditions{UID: &environment.UID}); err != nil {
		t.Fatal(err)
	}
	plan := h.savedPlan(t, stack, "Destroy")
	apply, job := h.activeApply(t, stack, plan)
	key := path.Join("runs", string(apply.UID), "terminal-result.json")
	h.setFault(t, []map[string]any{{"method": "PUT", "path": "^" + regexp.QuoteMeta("/"+h.namespace+"/"+key) + "$", "action": "status", "status": 403}})
	defer h.clearFault(t)
	terminal := h.applyTerminal(t, &apply, &job)
	if apply.Status.ExecutionOutcome != "Indeterminate" || apply.Status.EvidenceCaptured {
		t.Fatal("missing Destroy terminal was trusted")
	}
	if _, _, err := h.store.GetVerifiedByKey(h.ctx, key); !artifacts.IsNotFound(err) {
		t.Fatal("terminal was not actually missing: ", err)
	}
	after := h.cloudState(t, stack, false)
	for _, exists := range after {
		if exists {
			t.Fatal("fixture did not execute actual Terraform Destroy")
		}
	}
	if terminal["operation"] != "Apply" || terminal["terraformExitCode"] != float64(0) {
		t.Fatal("fixture was a Terraform failure rather than a missing terminal")
	}
	h.clearFault(t)
	h.retainFinalizers(t, stack, environment, "f02-missing-terminal")
	h.restartObserved(t, "f03-ambiguous-destroy")
	h.noRepeatedApply(t, stack, apply, job)
	h.retainFinalizers(t, stack, environment, "f03-after-restart")
	recordGateEvidence(t, h.namespace, "f02-f03-missing-destroy-result", map[string]any{"plan": plan, "apply": apply, "jobUID": job.UID, "terminalStdout": terminal, "missingObject": key, "cloudBefore": before, "cloudAfter": after, "destroyApplyCount": 1})
}

func (h *finalSafetyHarness) ambiguousApply(t *testing.T) {
	stack, environment := h.mutationEnvironment(t, "e-ambiguous", false)
	before := h.cloudState(t, stack, false)
	plan := h.savedPlan(t, stack, "Reconcile")
	apply, job := h.activeApply(t, stack, plan)
	key := path.Join("runs", string(apply.UID), "terminal-result.json")
	h.setFault(t, []map[string]any{{"method": "PUT", "path": "^" + regexp.QuoteMeta("/"+h.namespace+"/"+key) + "$", "action": "status", "status": 403}})
	defer h.clearFault(t)
	terminal := h.applyTerminal(t, &apply, &job)
	after := h.cloudState(t, stack, false)
	if apply.Status.ExecutionOutcome != "Indeterminate" || apply.Status.EvidenceCaptured {
		t.Fatal("ambiguous Apply became trusted success")
	}
	for _, exists := range after {
		if !exists {
			t.Fatal("actual Apply mutation was not observed")
		}
	}
	if _, _, err := h.store.GetVerifiedByKey(h.ctx, key); !artifacts.IsNotFound(err) {
		t.Fatal("terminal was not missing: ", err)
	}
	h.clearFault(t)
	h.restartObserved(t, "e04-ambiguous-apply")
	h.noRepeatedApply(t, stack, apply, job)
	h.retainFinalizers(t, stack, environment, "e04-ambiguous-apply")
	recordGateEvidence(t, h.namespace, "e04-ambiguous-apply-result", map[string]any{"plan": plan, "apply": apply, "jobUID": job.UID, "terminalStdout": terminal, "cloudBefore": before, "cloudAfter": after, "applyCount": 1})
}

func (h *finalSafetyHarness) partialApply(t *testing.T) {
	stack, environment := h.mutationEnvironment(t, "e-partial", true)
	before := h.cloudState(t, stack, true)
	plan := h.savedPlan(t, stack, "Reconcile")
	apply, job := h.activeApply(t, stack, plan)
	bucket := stack.Spec.Variables.Values["bucket_name"]
	h.cloudFault(t, "PUT", bucket+"-second")
	defer h.clearFault(t)
	terminal := h.applyTerminal(t, &apply, &job)
	after := h.cloudState(t, stack, true)
	if terminal["operation"] != "Apply" || terminal["terraformExitCode"] == float64(0) || terminal["mutationMayHaveOccurred"] != true || !after[bucket] || after[bucket+"-second"] {
		t.Fatalf("not a real partial Apply failure: terminal=%v cloud=%v", terminal, after)
	}
	h.clearFault(t)
	h.noRepeatedApply(t, stack, apply, job)
	h.retainFinalizers(t, stack, environment, "e05-partial-apply")
	recordGateEvidence(t, h.namespace, "e05-partial-apply-result", map[string]any{"plan": plan, "apply": apply, "jobUID": job.UID, "terminal": terminal, "cloudBefore": before, "cloudAfter": after, "applyCount": 1})
}

func (h *finalSafetyHarness) recoveryBlocked(t *testing.T) {
	if len(h.environments) == 0 {
		t.Skip("no mutation-fault environments in this explicit lane")
	}
	var retained []any
	for name, environment := range h.environments {
		var stack platform.InfraStack
		if err := h.kube.Get(h.ctx, client.ObjectKey{Namespace: h.namespace, Name: name}, &stack); err != nil {
			t.Fatal(err)
		}
		if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(environment), environment); err != nil {
			t.Fatal(err)
		}
		retained = append(retained, map[string]any{"stack": stack, "environment": environment, "normalDeleteAttempted": true})
	}
	recordGateEvidence(t, h.namespace, "f04-recovery-blocked", map[string]any{"status": "BLOCKED", "reason": "Controller deliberately requires supported fresh observation/recovery for post-mutation uncertainty; no approved force-release mechanism is invoked", "retained": retained, "manualFinalizerPatch": false, "forcedDelete": false})
	t.Skip("F04 normal recovery BLOCKED; uncertain resources and exact identities retained for supported operator recovery")
}
