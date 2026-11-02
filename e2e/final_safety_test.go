package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
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
	selected := os.Getenv("KPCP_FINAL_GATE")
	if selected != "" && selected != "A" && selected != "B" && selected != "E" && selected != "F" {
		t.Fatal("KPCP_FINAL_GATE must be A, B, E, F or empty for all")
	}
	h.group(t, selected, "A", "A_ContextDrift", h.contextDrift)
	h.group(t, selected, "B", "B_TerminalIdentity", h.terminalIdentity)
	h.group(t, selected, "B", "B_PartialUpload", h.partialUpload)
	h.group(t, selected, "B", "B_SnapshotDrift", h.snapshotDrift)
	if len(h.remaining) > 0 {
		recordGateEvidence(t, namespace, "final-safety-retained", h.remaining)
	}
	return h.clean && len(h.remaining) == 0
}

func (h *finalSafetyHarness) activePlan(t *testing.T, stack *platform.InfraStack) (platform.TerraformRun, batchv1.Job) {
	t.Helper()
	var run platform.TerraformRun
	var job batchv1.Job
	h.await(t, stack.Name+" real active Plan Job", func() (bool, error) {
		for _, candidate := range h.runs(t, stack.Name) {
			if candidate.Spec.Operation != "Plan" || candidate.Status.JobRef == nil || candidate.Status.JobUID == "" {
				continue
			}
			if err := h.kube.Get(h.ctx, client.ObjectKey{Namespace: h.namespace, Name: candidate.Status.JobRef.Name}, &job); err != nil {
				return false, client.IgnoreNotFound(err)
			}
			if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
				return false, fmt.Errorf("fixture Job finished before injection")
			}
			if string(job.UID) != candidate.Status.JobUID || !hasOwnerUID(job.OwnerReferences, candidate.UID) {
				return false, fmt.Errorf("fixture Job identity mismatch")
			}
			run = candidate
			return true, nil
		}
		return false, nil
	})
	return run, job
}

func (h *finalSafetyHarness) putTampered(t *testing.T, key string, body []byte) string {
	t.Helper()
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if _, err := h.storage.PutObject(h.ctx, &s3.PutObjectInput{Bucket: aws.String(h.namespace), Key: aws.String(key), Body: bytes.NewReader(body), Metadata: map[string]string{"sha256": digest}}); err != nil {
		t.Fatal(err)
	}
	ref, actual, err := h.store.GetVerifiedByKey(h.ctx, key)
	if err != nil || ref.Digest != digest || !bytes.Equal(actual, body) {
		t.Fatalf("test injection must have a valid content digest: %v", err)
	}
	return digest
}

func (h *finalSafetyHarness) terminalIdentity(t *testing.T) {
	for _, field := range []string{"run-uid", "job-uid", "operation", "plan-uid", "plan-digest"} {
		t.Run(field, func(t *testing.T) {
			stack := h.createStack(t, "b-terminal-"+field, nil)
			run, job := h.activePlan(t, stack)
			h.stop()
			paused := true
			defer func() {
				if paused {
					h.start()
				}
			}()
			h.await(t, "genuine runner terminal upload", func() (bool, error) {
				if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(&job), &job); err != nil {
					return false, err
				}
				if job.Status.Failed > 0 {
					return false, fmt.Errorf("fixture Plan Job failed before forgery")
				}
				return job.Status.Succeeded == 1, nil
			})
			terminalKey := path.Join("runs", string(run.UID), "terminal-result.json")
			originalRef, original, err := h.store.GetVerifiedByKey(h.ctx, terminalKey)
			if err != nil {
				t.Fatal(err)
			}
			var terminal map[string]any
			if err := json.Unmarshal(original, &terminal); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "run-uid":
				terminal["terraformRunUID"] = "00000000-0000-0000-0000-000000000001"
			case "job-uid":
				terminal["jobUID"] = "00000000-0000-0000-0000-000000000002"
			case "operation":
				terminal["operation"] = "Apply"
			case "plan-uid":
				planBytes, err := h.store.GetVerified(h.ctx, artifacts.Ref{Key: terminal["planRef"].(string), Digest: terminal["planDigest"].(string)})
				if err != nil {
					t.Fatal(err)
				}
				foreign := path.Join("runs", "qa-foreign-plan-uid", "plan.binary")
				h.putTampered(t, foreign, planBytes)
				terminal["planRef"] = foreign
			case "plan-digest":
				terminal["planDigest"] = "sha256:" + strings.Repeat("0", 64)
			}
			forged, _ := json.Marshal(terminal)
			forgedDigest := h.putTampered(t, terminalKey, forged)
			h.start()
			paused = false
			h.await(t, "controller classifies semantic terminal forgery", func() (bool, error) {
				if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(&run), &run); err != nil {
					return false, err
				}
				switch run.Status.ExecutionOutcome {
				case "ChangesPresent", "Failed", "Rejected", "Indeterminate":
					return true, nil
				}
				return false, nil
			})
			recordGateEvidence(t, h.namespace, "b-terminal-"+field+"-result", map[string]any{"run": run, "job": job, "originalTerminalRef": originalRef, "forgedTerminalDigest": forgedDigest, "forgedTerminal": terminal, "contentHashVerified": true})
			if run.Status.ExecutionOutcome == "ChangesPresent" || run.Status.EvidenceCaptured && run.Status.ArtifactsReady {
				t.Errorf("digest-correct terminal with wrong %s was accepted", field)
			}
			h.noApply(t, stack, "forgery-no-mutation")
			h.removeUnapplied(t, stack)
		})
	}
}

func (h *finalSafetyHarness) setFault(t *testing.T, rules []map[string]any) {
	t.Helper()
	if !filepath.IsAbs(h.cfg.FinalSafetyFaultFile) {
		t.Fatal("supply a new absolute test-owned FinalSafetyFaultFile")
	}
	data, _ := json.Marshal(map[string]any{"runID": h.namespace, "rules": rules})
	temporary := h.cfg.FinalSafetyFaultFile + ".next"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, h.cfg.FinalSafetyFaultFile); err != nil {
		t.Fatal(err)
	}
}

func (h *finalSafetyHarness) clearFault(t *testing.T) {
	t.Helper()
	if err := os.Remove(h.cfg.FinalSafetyFaultFile); err != nil && !os.IsNotExist(err) {
		h.clean = false
		t.Error(err)
	}
}

func (h *finalSafetyHarness) partialUpload(t *testing.T) {
	for _, mode := range []string{"interrupted", "missing-terminal", "uncertain-write"} {
		t.Run(mode, func(t *testing.T) {
			stack := h.createStack(t, "b-upload-"+mode, nil)
			run, _ := h.activePlan(t, stack)
			key := path.Join("runs", string(run.UID), "plan-report.json")
			action := "abort_before"
			if mode == "missing-terminal" {
				key = path.Join("runs", string(run.UID), "terminal-result.json")
				action = "status"
			}
			if mode == "uncertain-write" {
				action = "abort_after"
			}
			h.setFault(t, []map[string]any{{"method": "PUT", "path": "^" + regexp.QuoteMeta("/"+h.namespace+"/"+key) + "$", "action": action, "status": 403}})
			defer h.clearFault(t)
			h.await(t, "partial upload cannot become success", func() (bool, error) {
				if err := h.kube.Get(h.ctx, client.ObjectKeyFromObject(&run), &run); err != nil {
					return false, err
				}
				if run.Status.ExecutionOutcome == "ChangesPresent" || run.Status.ExecutionOutcome == "Succeeded" {
					return false, fmt.Errorf("incomplete upload became trusted success")
				}
				return run.Status.ExecutionOutcome == "Failed" || run.Status.ExecutionOutcome == "Indeterminate", nil
			})
			h.noApply(t, stack, "partial-upload-blocked")
			recordGateEvidence(t, h.namespace, "b-upload-"+mode+"-result", map[string]any{"run": run, "faultObject": key, "action": action, "noFalseSuccess": true})
			h.clearFault(t)
			if mode == "uncertain-write" {
				ref, body, err := h.store.GetVerifiedByKey(h.ctx, key)
				if err != nil {
					t.Fatal("uncertain response did not leave the expected independently readable object: ", err)
				}
				retry, err := h.store.PutImmutable(h.ctx, key, body)
				if err != nil || retry.Digest != ref.Digest {
					t.Fatal("identical immutable retry failed: ", err)
				}
				if _, err := h.store.PutImmutable(h.ctx, key, []byte("different QA bytes")); err != artifacts.ErrArtifactExists {
					t.Fatal("conflicting retry was accepted: ", err)
				}
				recordGateEvidence(t, h.namespace, "b-upload-immutable-retry", map[string]any{"original": ref, "retry": retry, "conflictRejected": true})
			}
			var fresh platform.TerraformRun
			h.await(t, "recovered Plan has its own new UID", func() (bool, error) {
				for _, candidate := range h.runs(t, stack.Name) {
					if candidate.Spec.Operation == "Plan" && candidate.UID != run.UID && candidate.Status.ExecutionOutcome == "ChangesPresent" {
						fresh = candidate
						return true, nil
					}
				}
				return false, nil
			})
			recordGateEvidence(t, h.namespace, "b-upload-"+mode+"-recovery", map[string]any{"failedPlan": run, "freshPlan": fresh, "differentUID": run.UID != fresh.UID})
			h.removeUnapplied(t, stack)
		})
	}
}

func (h *finalSafetyHarness) snapshotDrift(t *testing.T) {
	for _, kind := range []string{"source", "backend"} {
		t.Run(kind, func(t *testing.T) {
			stack := h.createStack(t, "b-snapshot-"+kind, nil)
			plan := h.savedPlan(t, stack, "Reconcile")
			key, approvedDigest := plan.Status.SourceBundleRef, plan.Status.SourceBundleDigest
			if kind == "backend" {
				key, approvedDigest = plan.Status.ResolvedBackendConfigRef, plan.Status.ResolvedBackendConfigDigest
			}
			actualDigest := h.putTampered(t, key, []byte("test-owned changed "+kind+" snapshot"))
			approval := h.approve(t, plan, "b-snapshot-"+kind+"-approval")
			var rejected platform.TerraformRun
			h.await(t, "changed snapshot rejects Apply before Job", func() (bool, error) {
				for _, run := range h.runs(t, stack.Name) {
					if run.Spec.Operation == "Apply" {
						if run.Status.JobRef != nil {
							return false, fmt.Errorf("changed snapshot started Apply Job")
						}
						if run.Status.ExecutionOutcome == "Rejected" {
							rejected = run
							return true, nil
						}
					}
				}
				return false, nil
			})
			if _, err := h.storage.HeadBucket(h.ctx, &s3.HeadBucketInput{Bucket: aws.String(stack.Spec.Variables.Values["bucket_name"])}); !artifacts.IsNotFound(err) {
				t.Fatal("changed snapshot mutated cloud: ", err)
			}
			recordGateEvidence(t, h.namespace, "b-snapshot-"+kind+"-rejected", map[string]any{"plan": plan, "approval": approval, "rejectedApply": rejected, "artifactKey": key, "approvedDigest": approvedDigest, "actualVerifiedDigest": actualDigest, "applyJobCount": 0, "cloudAbsent": true})
			h.removeUnapplied(t, stack)
		})
	}
}

func (h *finalSafetyHarness) group(t *testing.T, selected, gate, name string, run func(*testing.T)) {
	t.Run(name, func(t *testing.T) {
		if selected != "" && selected != gate {
			t.Skip("explicit KPCP_FINAL_GATE selection")
		}
		run(t)
	})
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
