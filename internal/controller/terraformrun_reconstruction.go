package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
)

// ReconstructActiveTerraformRuns rebuilds execution truth from the management
// API before controllers start admitting new work. Jobs are authoritative for
// activity; TerraformRun supplies the immutable operation and stack binding.
func ReconstructActiveTerraformRuns(ctx context.Context, reader client.Reader, kubeClient kubernetes.Interface, gate *reliability.Gate) (reliability.ReconstructionResult, error) {
	if reader == nil || kubeClient == nil || gate == nil {
		return reliability.ReconstructionResult{}, fmt.Errorf("reader, kube client, and execution gate are required")
	}
	var runs platformv1alpha1.TerraformRunList
	if err := reader.List(ctx, &runs); err != nil {
		return reliability.ReconstructionResult{}, fmt.Errorf("list TerraformRuns: %w", err)
	}
	runsByUID := make(map[string]*platformv1alpha1.TerraformRun, len(runs.Items))
	for index := range runs.Items {
		run := &runs.Items[index]
		if run.UID != "" {
			runsByUID[string(run.UID)] = run
		}
	}
	jobs, err := kubeClient.BatchV1().Jobs("").List(ctx, metav1.ListOptions{LabelSelector: "platform.example.io/terraform-run-uid"})
	if err != nil {
		return reliability.ReconstructionResult{}, fmt.Errorf("list Terraform Jobs: %w", err)
	}
	items := make([]reliability.WorkItem, 0, len(jobs.Items))
	for index := range jobs.Items {
		job := &jobs.Items[index]
		if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
			continue
		}
		runUID := job.Labels["platform.example.io/terraform-run-uid"]
		run := runsByUID[runUID]
		if run == nil {
			for _, owner := range job.OwnerReferences {
				if owner.Kind == "TerraformRun" {
					run = runsByUID[string(owner.UID)]
					break
				}
			}
		}
		if run == nil {
			// An active runner with no matching TerraformRun is not safe to
			// classify. Reconstruct marks the invalid item as a safety fault.
			items = append(items, reliability.WorkItem{ID: job.Namespace + "/" + job.Name})
			continue
		}
		items = append(items, workItemForTerraformRun(run))
	}
	return gate.Reconstruct(items), nil
}

func workItemForTerraformRun(run *platformv1alpha1.TerraformRun) reliability.WorkItem {
	kind := reliability.KindPlan
	if run.Spec.Operation == "Apply" {
		kind = reliability.KindApply
	}
	id := string(run.UID)
	if id == "" {
		id = run.Namespace + "/" + run.Name
	}
	return reliability.WorkItem{ID: id, Kind: kind, Stack: run.Spec.StackRef.Name}
}
