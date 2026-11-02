package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	platform "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The existing lifecycle lane supplies an already exercised controller, real
// storage and fixed runner/source. Each attack gets a separately owned stack;
// none changes a shared cluster setting or removes a business finalizer.
type finalSafetyHarness struct {
	ctx         context.Context
	kube        client.Client
	storage     *s3.Client
	store       *artifacts.Store
	cfg         configuration
	namespace   string
	template    platform.InfraStack
	stop, start func()
	clean       bool
	remaining   map[string]bool
}

func runFinalSafety(t *testing.T, ctx context.Context, kube client.Client, storage *s3.Client, store *artifacts.Store, cfg configuration, namespace string, template platform.InfraStack, stop, start func()) bool {
	h := &finalSafetyHarness{ctx: ctx, kube: kube, storage: storage, store: store, cfg: cfg, namespace: namespace, template: template, stop: stop, start: start, clean: true, remaining: map[string]bool{}}
	t.Run("A_ContextDrift", h.contextDrift)
	if len(h.remaining) > 0 {
		recordGateEvidence(t, namespace, "final-safety-retained", h.remaining)
	}
	return h.clean && len(h.remaining) == 0
}

func (h *finalSafetyHarness) await(t *testing.T, description string, check func() (bool, error)) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	for {
		ok, err := check()
		if err != nil {
			t.Fatalf("%s: %v", description, err)
		}
		if ok {
			return
		}
		select {
		case <-h.ctx.Done():
			t.Fatalf("%s: %v", description, h.ctx.Err())
		case <-deadline.C:
			t.Fatalf("%s: timed out", description)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (h *finalSafetyHarness) createStack(t *testing.T, name string, configure func(*platform.InfraStack)) *platform.InfraStack {
	t.Helper()
	stack := h.template.DeepCopy()
	stack.ObjectMeta = metav1.ObjectMeta{Name: name, Namespace: h.namespace, Labels: map[string]string{"platform.example.io/e2e": h.namespace}}
	stack.Status = platform.InfraStackStatus{}
	stack.Spec.OwnerEnvironmentUID = ""
	stack.Spec.Workspace = name
	stack.Spec.Variables.Values["bucket_name"] = h.namespace + "-" + name + "-cloud"
	stack.Spec.MutationFence = false
	stack.Spec.DesiredState = platform.DesiredStatePresent
	if configure != nil {
		configure(stack)
	}
	if err := h.kube.Create(h.ctx, stack); err != nil {
		t.Fatal(err)
	}
	h.remaining[stack.Name] = true
	recordGateEvidence(t, h.namespace, name+"-created", stack)
	return stack
}

func (h *finalSafetyHarness) runs(t *testing.T, name string) []platform.TerraformRun {
	t.Helper()
	var list platform.TerraformRunList
	if err := h.kube.List(h.ctx, &list, client.InNamespace(h.namespace)); err != nil {
		t.Fatal(err)
	}
	var result []platform.TerraformRun
	for _, run := range list.Items {
		if run.Spec.StackRef.Name == name {
			result = append(result, run)
		}
	}
	return result
}

func (h *finalSafetyHarness) savedPlan(t *testing.T, stack *platform.InfraStack, mode string) platform.TerraformRun {
	t.Helper()
	var plan platform.TerraformRun
	h.await(t, stack.Name+" saved "+mode+" Plan", func() (bool, error) {
		for _, run := range h.runs(t, stack.Name) {
			if run.Spec.Operation != "Plan" || run.Spec.PlanMode != mode {
				continue
			}
			if run.Status.ExecutionOutcome == "ChangesPresent" && run.Status.ArtifactsReady && run.Status.EvidenceCaptured {
				plan = run
				return true, nil
			}
			if run.Status.ExecutionOutcome == "Failed" || run.Status.ExecutionOutcome == "Rejected" || run.Status.ExecutionOutcome == "Indeterminate" {
				return false, fmt.Errorf("Plan UID=%s outcome=%s conditions=%+v", run.UID, run.Status.ExecutionOutcome, run.Status.Conditions)
			}
		}
		return false, nil
	})
	recordGateEvidence(t, h.namespace, stack.Name+"-saved-plan", plan)
	return plan
}

func (h *finalSafetyHarness) approve(t *testing.T, plan platform.TerraformRun, name string) *platform.ChangeApproval {
	t.Helper()
	approval := &platform.ChangeApproval{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.namespace}, Spec: platform.ChangeApprovalSpec{PlanRunRef: corev1.LocalObjectReference{Name: plan.Name}, PlanRunUID: string(plan.UID), PlanDigest: plan.Status.PlanDigest, ExecutionContextDigest: plan.Spec.ExecutionContextDigest, EffectivePlanInputDigest: plan.Status.EffectivePlanInputDigest, PlanReportRef: plan.Status.PlanReportRef, PlanReportDigest: plan.Status.PlanReportDigest}}
	if err := h.kube.Create(h.ctx, approval); err != nil {
		t.Fatal(err)
	}
	return approval
}

func (h *finalSafetyHarness) noApply(t *testing.T, stack *platform.InfraStack, phase string) {
	t.Helper()
	runs := h.runs(t, stack.Name)
	for _, run := range runs {
		if run.Spec.Operation == "Apply" {
			t.Fatalf("%s admitted Apply UID=%s", phase, run.UID)
		}
	}
	var all batchv1.JobList
	if err := h.kube.List(h.ctx, &all, client.InNamespace(h.namespace)); err != nil {
		t.Fatal(err)
	}
	var jobs []batchv1.Job
	for _, job := range all.Items {
		for _, run := range runs {
			if hasOwnerUID(job.OwnerReferences, run.UID) {
				if run.Spec.Operation != "Plan" {
					t.Fatal("unexpected mutation Job")
				}
				jobs = append(jobs, job)
			}
		}
	}
	if _, err := h.storage.HeadBucket(h.ctx, &s3.HeadBucketInput{Bucket: aws.String(h.namespace + "-" + stack.Name + "-cloud")}); !artifacts.IsNotFound(err) {
		t.Fatal("unexpected cloud mutation: ", err)
	}
	recordGateEvidence(t, h.namespace, stack.Name+"-"+phase, map[string]any{"stack": stack, "runs": runs, "jobs": jobs, "applyCount": 0, "cloudAbsent": true})
}

func (h *finalSafetyHarness) removeUnapplied(t *testing.T, stack *platform.InfraStack) {
	t.Helper()
	if err := h.kube.Delete(h.ctx, stack, client.Preconditions{UID: &stack.UID}); err != nil {
		h.clean = false
		t.Error(err)
		return
	}
	h.await(t, stack.Name+" ordinary finalizer cleanup", func() (bool, error) {
		err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(stack), &platform.InfraStack{})
		return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
	})
	delete(h.remaining, stack.Name)
}

func (h *finalSafetyHarness) contextDrift(t *testing.T) {
	for _, field := range []string{"source", "target", "backend", "runner", "inputs", "profile"} {
		t.Run(field, func(t *testing.T) {
			stack := h.createStack(t, "a-"+field, nil)
			plan := h.savedPlan(t, stack, "Reconcile")
			if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(stack), stack); err != nil {
				t.Fatal(err)
			}
			before := stack.DeepCopy()
			switch field {
			case "source":
				if len(h.cfg.FinalSafetySourceRevision) != 40 || h.cfg.FinalSafetySourceRevision == h.cfg.SourceRevision {
					t.Fatal("supply a different existing pinned fixture commit")
				}
				stack.Spec.Source.Revision = h.cfg.FinalSafetySourceRevision
			case "target":
				stack.Spec.RuntimeTargetIdentity.IncarnationID = "qa-controlled-replacement"
			case "backend":
				var backend corev1.ConfigMap
				if err := h.kube.Get(h.ctx, client.ObjectKey{Namespace: h.namespace, Name: stack.Spec.Backend.ConfigRef.Name}, &backend); err != nil {
					t.Fatal(err)
				}
				backend.ObjectMeta = metav1.ObjectMeta{Name: "a-backend-replacement", Namespace: h.namespace}
				if err := h.kube.Create(h.ctx, &backend); err != nil {
					t.Fatal(err)
				}
				stack.Spec.Backend.ConfigRef.Name = backend.Name
			case "runner":
				stack.Spec.Executor.Image = "localhost/kpcp-runner@sha256:" + strings.Repeat("0", 64)
			case "inputs":
				stack.Spec.Variables.Values["bucket_name"] += "-changed"
			case "profile":
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "a-profile-replacement", Namespace: h.namespace}, Data: map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("000000000000"), "AWS_SECRET_ACCESS_KEY": []byte("test")}}
				if err := h.kube.Create(h.ctx, secret); err != nil {
					t.Fatal(err)
				}
				stack.Spec.ExecutionProfile.CredentialsSecretRef.Name = secret.Name
			}
			if err := h.kube.Update(h.ctx, stack); err != nil {
				t.Fatal(err)
			}
			approval := h.approve(t, plan, "a-"+field+"-old-approval")
			h.await(t, field+" controller observes changed execution context", func() (bool, error) {
				if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(stack), stack); err != nil {
					return false, err
				}
				for _, run := range h.runs(t, stack.Name) {
					if run.Spec.Operation == "Plan" && run.UID != plan.UID && run.Spec.InfraStackGeneration == stack.Generation {
						return true, nil
					}
				}
				return false, nil
			})
			time.Sleep(2 * time.Second)
			h.noApply(t, stack, "old-approval-blocked")
			recordGateEvidence(t, h.namespace, "a-"+field+"-context", map[string]any{"before": before, "after": stack, "savedPlan": plan, "oldApproval": approval})
			h.removeUnapplied(t, stack)
		})
	}
}
