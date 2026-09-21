package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (in *ExecutorSpec) DeepCopyInto(out *ExecutorSpec) {
	*out = *in
	if in.Parallelism != nil {
		out.Parallelism = new(int32)
		*out.Parallelism = *in.Parallelism
	}
}

func (in *VariablesSpec) DeepCopyInto(out *VariablesSpec) {
	*out = *in
	if in.SecretRefs != nil {
		out.SecretRefs = make([]corev1.LocalObjectReference, len(in.SecretRefs))
		copy(out.SecretRefs, in.SecretRefs)
	}
}

func (in *InfraStackSpec) DeepCopyInto(out *InfraStackSpec) {
	*out = *in
	in.Variables.DeepCopyInto(&out.Variables)
	in.Executor.DeepCopyInto(&out.Executor)
}

func (in *PlatformEnvironmentStatus) DeepCopyInto(out *PlatformEnvironmentStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
	if in.InfraStackRef != nil {
		out.InfraStackRef = in.InfraStackRef.DeepCopy()
	}
	if in.ResourceSetRef != nil {
		out.ResourceSetRef = in.ResourceSetRef.DeepCopy()
	}
}

func (in *InfraStackStatus) DeepCopyInto(out *InfraStackStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
	if in.LatestPlanRunRef != nil {
		out.LatestPlanRunRef = in.LatestPlanRunRef.DeepCopy()
	}
	if in.LastAppliedRunRef != nil {
		out.LastAppliedRunRef = in.LastAppliedRunRef.DeepCopy()
	}
	if in.DiscoveredRuntimeTargetIdentity != nil {
		out.DiscoveredRuntimeTargetIdentity = in.DiscoveredRuntimeTargetIdentity.DeepCopy()
	}
	if in.DiscoveredTargetConnectionProfile != nil {
		out.DiscoveredTargetConnectionProfile = in.DiscoveredTargetConnectionProfile.DeepCopy()
	}
}

func (in *TerraformRunSpec) DeepCopyInto(out *TerraformRunSpec) {
	*out = *in
	if in.ApprovalRef != nil {
		out.ApprovalRef = in.ApprovalRef.DeepCopy()
	}
	in.Executor.DeepCopyInto(&out.Executor)
}

func (in *TerraformRunStatus) DeepCopyInto(out *TerraformRunStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
	if in.PlanCreatedAt != nil {
		out.PlanCreatedAt = in.PlanCreatedAt.DeepCopy()
	}
	if in.PlanExpiresAt != nil {
		out.PlanExpiresAt = in.PlanExpiresAt.DeepCopy()
	}
	if in.JobRef != nil {
		out.JobRef = in.JobRef.DeepCopy()
	}
}

func (in *ResourceSetStatus) DeepCopyInto(out *ResourceSetStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
}

func (in *ChangeApprovalStatus) DeepCopyInto(out *ChangeApprovalStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
	if in.ApprovedAt != nil {
		out.ApprovedAt = in.ApprovedAt.DeepCopy()
	}
}

func (in *ValkeyClusterStatus) DeepCopyInto(out *ValkeyClusterStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
}
