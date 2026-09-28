package console

import (
	"context"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/planview"
)

type ConditionView struct {
	Type               string                 `json:"type"`
	Status             metav1.ConditionStatus `json:"status"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
}

type TargetView struct {
	Provider      string `json:"provider,omitempty"`
	AccountID     string `json:"accountID,omitempty"`
	Region        string `json:"region,omitempty"`
	ClusterName   string `json:"clusterName,omitempty"`
	IncarnationID string `json:"incarnationID,omitempty"`
}

type RunBrief struct {
	Name       string `json:"name"`
	Operation  string `json:"operation"`
	Outcome    string `json:"outcome,omitempty"`
	HasChanges bool   `json:"hasChanges,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type EnvironmentSummary struct {
	Namespace          string                 `json:"namespace"`
	Name               string                 `json:"name"`
	UID                string                 `json:"uid"`
	Class              string                 `json:"class,omitempty"`
	Generation         int64                  `json:"generation"`
	NodeCount          int32                  `json:"nodeCount"`
	Phase              string                 `json:"phase"`
	Ready              bool                   `json:"ready"`
	ReadyStatus        metav1.ConditionStatus `json:"readyStatus"`
	Infrastructure     string                 `json:"infrastructure"`
	Runtime            string                 `json:"runtime"`
	Target             TargetView             `json:"target"`
	LatestTerraformRun *RunBrief              `json:"latestTerraformRun,omitempty"`
	LatestApproval     string                 `json:"latestApproval,omitempty"`
	LastTransitionTime *metav1.Time           `json:"lastTransitionTime,omitempty"`
	CreatedAt          metav1.Time            `json:"createdAt"`
	Deleting           bool                   `json:"deleting,omitempty"`
}

type ClassView struct {
	Name           string                          `json:"name"`
	Provider       string                          `json:"provider"`
	TargetCluster  string                          `json:"targetCluster"`
	DefaultRegion  string                          `json:"defaultRegion,omitempty"`
	AllowedRegions []string                        `json:"allowedRegions,omitempty"`
	CapacityBounds platformv1alpha1.CapacityBounds `json:"capacityBounds"`
	Ready          bool                            `json:"ready"`
	ReadyReason    string                          `json:"readyReason,omitempty"`
}

type InfraView struct {
	Name                string                        `json:"name"`
	DesiredState        platformv1alpha1.DesiredState `json:"desiredState,omitempty"`
	State               string                        `json:"state"`
	Condition           *ConditionView                `json:"condition,omitempty"`
	LatestPlanRun       string                        `json:"latestPlanRun,omitempty"`
	LastAppliedRun      string                        `json:"lastAppliedRun,omitempty"`
	ConvergenceEvidence bool                          `json:"convergenceEvidence"`
}

type RuntimeView struct {
	Name                   string         `json:"name"`
	State                  string         `json:"state"`
	Condition              *ConditionView `json:"condition,omitempty"`
	InventoryItems         int32          `json:"inventoryItems"`
	InventoryLimitExceeded bool           `json:"inventoryLimitExceeded"`
	MutationBlocked        bool           `json:"mutationBlocked"`
	CleanupEvidence        bool           `json:"cleanupEvidence"`
}

type ApprovalView struct {
	Namespace  string       `json:"namespace"`
	Name       string       `json:"name"`
	PlanRun    string       `json:"planRun"`
	PlanRunUID string       `json:"planRunUID"`
	State      string       `json:"state"`
	PlanDigest string       `json:"planDigest"`
	CreatedAt  metav1.Time  `json:"createdAt"`
	ApprovedAt *metav1.Time `json:"approvedAt,omitempty"`
}

type TerraformRunView struct {
	Namespace                string        `json:"namespace"`
	Name                     string        `json:"name"`
	UID                      string        `json:"uid"`
	Operation                string        `json:"operation"`
	PlanMode                 string        `json:"planMode,omitempty"`
	State                    string        `json:"state"`
	Reason                   string        `json:"reason,omitempty"`
	Message                  string        `json:"message,omitempty"`
	Outcome                  string        `json:"outcome,omitempty"`
	HasChanges               bool          `json:"hasChanges"`
	PlanDigest               string        `json:"planDigest,omitempty"`
	PlanRef                  string        `json:"planRef,omitempty"`
	PlanReportRef            string        `json:"planReportRef,omitempty"`
	PlanReportDigest         string        `json:"planReportDigest,omitempty"`
	EffectivePlanInputDigest string        `json:"effectivePlanInputDigest,omitempty"`
	ExecutionContextDigest   string        `json:"executionContextDigest,omitempty"`
	PlanExpiresAt            *metav1.Time  `json:"planExpiresAt,omitempty"`
	PlanCreatedAt            *metav1.Time  `json:"planCreatedAt,omitempty"`
	ArtifactsReady           bool          `json:"artifactsReady"`
	EvidenceCaptured         bool          `json:"evidenceCaptured"`
	TerminalStartedAt        *metav1.Time  `json:"terminalStartedAt,omitempty"`
	TerminalFinishedAt       *metav1.Time  `json:"terminalFinishedAt,omitempty"`
	Approval                 *ApprovalView `json:"approval,omitempty"`
	CreatedAt                metav1.Time   `json:"createdAt"`
}

type TimelineEvent struct {
	At       metav1.Time `json:"at"`
	Kind     string      `json:"kind"`
	Resource string      `json:"resource"`
	Title    string      `json:"title"`
	State    string      `json:"state,omitempty"`
	Reason   string      `json:"reason,omitempty"`
	Message  string      `json:"message,omitempty"`
}

type EvidenceView struct {
	PlanDigest            string `json:"planDigest,omitempty"`
	PlanReportRef         string `json:"planReportRef,omitempty"`
	PlanReportDigest      string `json:"planReportDigest,omitempty"`
	TerminalResultRef     string `json:"terminalResultRef,omitempty"`
	TerminalResultDigest  string `json:"terminalResultDigest,omitempty"`
	SourceBundleDigest    string `json:"sourceBundleDigest,omitempty"`
	TargetDiscoveryDigest string `json:"targetDiscoveryDigest,omitempty"`
}

type EnvironmentDetail struct {
	EnvironmentSummary
	Conditions           []ConditionView    `json:"conditions"`
	InfrastructureDetail *InfraView         `json:"infrastructureDetail,omitempty"`
	RuntimeDetail        *RuntimeView       `json:"runtimeDetail,omitempty"`
	TerraformRuns        []TerraformRunView `json:"terraformRuns"`
	Approvals            []ApprovalView     `json:"approvals"`
	Valkey               *ValkeyView        `json:"valkey,omitempty"`
	Evidence             EvidenceView       `json:"evidence"`
	Timeline             []TimelineEvent    `json:"timeline"`
	TimelineNote         string             `json:"timelineNote"`
}

type ValkeyView struct {
	Desired       bool   `json:"desired"`
	Shards        int32  `json:"shards,omitempty"`
	Replicas      int32  `json:"replicas,omitempty"`
	ReadyReplicas int32  `json:"readyReplicas,omitempty"`
	State         string `json:"state"`
}

type PlanView struct {
	TerraformRun *TerraformRunView `json:"terraformRun,omitempty"`
	Available    bool              `json:"available"`
	Message      string            `json:"message"`
	Graph        *planGraphView    `json:"graph,omitempty"`
}

// Aliasing the plan model keeps the API response identical to the immutable
// artifact while avoiding exposure of the raw Terraform plan JSON.
type planGraphView = planview.Document

type snapshot struct {
	environments platformv1alpha1.PlatformEnvironmentList
	classes      platformv1alpha1.EnvironmentClassList
	stacks       platformv1alpha1.InfraStackList
	runs         platformv1alpha1.TerraformRunList
	approvals    platformv1alpha1.ChangeApprovalList
	resourceSets platformv1alpha1.ResourceSetList
	valkeys      platformv1alpha1.ValkeyClusterList
}

func (s *Server) loadSnapshot(ctx context.Context) (*snapshot, error) {
	result := &snapshot{}
	requests := []struct {
		name string
		list client.ObjectList
	}{
		{"PlatformEnvironments", &result.environments}, {"EnvironmentClasses", &result.classes}, {"InfraStacks", &result.stacks},
		{"TerraformRuns", &result.runs}, {"ChangeApprovals", &result.approvals}, {"ResourceSets", &result.resourceSets}, {"ValkeyClusters", &result.valkeys},
	}
	for _, request := range requests {
		if err := s.client.List(ctx, request.list); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (data *snapshot) detail(environment *platformv1alpha1.PlatformEnvironment) EnvironmentDetail {
	class := findClass(data.classes.Items, className(environment))
	stack := findStack(data.stacks.Items, environment)
	resourceSet := findResourceSet(data.resourceSets.Items, environment)
	runs := make([]*platformv1alpha1.TerraformRun, 0)
	for i := range data.runs.Items {
		run := &data.runs.Items[i]
		if run.Namespace == environment.Namespace && stack != nil && (run.Spec.StackRef.Name == stack.Name || hasOwnerUID(run, stack.UID)) {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CreationTimestamp.Equal(&runs[j].CreationTimestamp) {
			return runs[i].Name < runs[j].Name
		}
		return runs[i].CreationTimestamp.Before(&runs[j].CreationTimestamp)
	})
	approvals := make([]ApprovalView, 0)
	views := make([]TerraformRunView, 0, len(runs))
	for _, run := range runs {
		related := matchingApprovals(data.approvals.Items, run)
		for _, approval := range related {
			approvals = append(approvals, approvalView(approval))
		}
		views = append(views, runView(run, related))
	}
	summary := environmentSummary(environment, class, stack, resourceSet, runs, approvals)
	detail := EnvironmentDetail{EnvironmentSummary: summary, Conditions: conditionViews(environment.Status.Conditions), TerraformRuns: views, Approvals: approvals, Timeline: buildTimeline(environment, stack, resourceSet, runs, approvals), TimelineNote: "Timeline entries are reconstructed from current Kubernetes conditions and immutable run/approval timestamps; the CRDs do not retain every historical condition transition."}
	if stack != nil {
		readyCondition := findReady(stack.Status.Conditions)
		state := "Unknown"
		if readyCondition != nil {
			state = conditionState(*readyCondition)
		}
		infra := &InfraView{Name: stack.Name, DesiredState: stack.Spec.DesiredState, State: state, Condition: conditionView(readyCondition), ConvergenceEvidence: stack.Status.ConvergenceEvidenceRef != "" && stack.Status.ConvergenceEvidenceDigest != ""}
		if stack.Status.LatestPlanRunRef != nil {
			infra.LatestPlanRun = stack.Status.LatestPlanRunRef.Name
		}
		if stack.Status.LastAppliedRunRef != nil {
			infra.LastAppliedRun = stack.Status.LastAppliedRunRef.Name
		}
		detail.InfrastructureDetail = infra
		if summary.LatestTerraformRun == nil && len(runs) > 0 {
			latest := runs[len(runs)-1]
			summary.LatestTerraformRun = briefRun(latest)
		}
	}
	if resourceSet != nil {
		condition := findReady(resourceSet.Status.Conditions)
		state := "Unknown"
		if condition != nil {
			state = conditionState(*condition)
		}
		detail.RuntimeDetail = &RuntimeView{Name: resourceSet.Name, State: state, Condition: conditionView(condition), InventoryItems: resourceSet.Status.InventoryItems, InventoryLimitExceeded: resourceSet.Status.InventoryLimitExceeded, MutationBlocked: resourceSet.Status.MutationBlocked, CleanupEvidence: resourceSet.Status.CleanupEvidenceRef != "" && resourceSet.Status.CleanupEvidenceDigest != ""}
	}
	if stack != nil {
		if identity := environment.Status.TargetIdentity; identity != nil {
			detail.Target = targetFromIdentity(identity)
		} else if identity := stack.Status.DiscoveredRuntimeTargetIdentity; identity != nil {
			detail.Target = targetFromIdentity(identity)
		} else {
			detail.Target = targetFromIdentity(&stack.Spec.RuntimeTargetIdentity)
		}
	}
	if environment.Spec.Valkey.Enabled {
		valkey := &ValkeyView{Desired: true, Shards: environment.Spec.Valkey.Shards, Replicas: environment.Spec.Valkey.Replicas, State: "Requested"}
		for i := range data.valkeys.Items {
			candidate := &data.valkeys.Items[i]
			if candidate.Namespace == environment.Namespace && hasOwnerUID(candidate, environment.UID) {
				valkey.ReadyReplicas = candidate.Status.ReadyReplicas
				valkey.State = "Pending"
				if meta.IsStatusConditionTrue(candidate.Status.Conditions, "Ready") {
					valkey.State = "Ready"
				}
				break
			}
		}
		detail.Valkey = valkey
	}
	if len(runs) > 0 {
		latest := runs[len(runs)-1]
		detail.Evidence = EvidenceView{PlanDigest: latest.Status.PlanDigest, PlanReportRef: latest.Status.PlanReportRef, PlanReportDigest: latest.Status.PlanReportDigest, TerminalResultRef: latest.Status.TerminalResultRef, TerminalResultDigest: latest.Status.TerminalResultDigest, SourceBundleDigest: latest.Status.SourceBundleDigest, TargetDiscoveryDigest: latest.Status.TargetDiscoveryDigest}
	}
	return detail
}

func classView(class *platformv1alpha1.EnvironmentClass) ClassView {
	condition := findReady(class.Status.Conditions)
	view := ClassView{Name: class.Name, Provider: class.Spec.Target.Provider, TargetCluster: class.Spec.Target.ClusterName, DefaultRegion: class.Spec.Target.Region, AllowedRegions: append([]string(nil), class.Spec.AllowedRegions...), CapacityBounds: class.Spec.CapacityBounds, Ready: meta.IsStatusConditionTrue(class.Status.Conditions, "Ready")}
	if condition != nil {
		view.ReadyReason = condition.Reason
	}
	return view
}

func environmentSummary(environment *platformv1alpha1.PlatformEnvironment, class *platformv1alpha1.EnvironmentClass, stack *platformv1alpha1.InfraStack, resourceSet *platformv1alpha1.ResourceSet, runs []*platformv1alpha1.TerraformRun, approvals []ApprovalView) EnvironmentSummary {
	ready := findReady(environment.Status.Conditions)
	readyStatus := metav1.ConditionUnknown
	readyValue := false
	phase := "Reconciling"
	lastTransition := (*metav1.Time)(nil)
	if ready != nil {
		readyStatus = ready.Status
		readyValue = ready.Status == metav1.ConditionTrue
		phase = conditionState(*ready)
		copyTime := ready.LastTransitionTime
		lastTransition = &copyTime
	}
	if !environment.DeletionTimestamp.IsZero() {
		phase = "Deleting"
	}
	if ready == nil && environment.Status.ObservedGeneration < environment.Generation {
		phase = "Pending"
	}
	infraState := "NotCreated"
	if stack != nil {
		if condition := findReady(stack.Status.Conditions); condition != nil {
			infraState = conditionState(*condition)
		} else {
			infraState = "Pending"
		}
	}
	runtimeState := "NotCreated"
	if resourceSet != nil {
		if condition := findReady(resourceSet.Status.Conditions); condition != nil {
			runtimeState = conditionState(*condition)
		} else {
			runtimeState = "Pending"
		}
	}
	target := TargetView{}
	if identity := environment.Status.TargetIdentity; identity != nil {
		target = targetFromIdentity(identity)
	} else if stack != nil {
		if identity := stack.Status.DiscoveredRuntimeTargetIdentity; identity != nil {
			target = targetFromIdentity(identity)
		} else {
			target = targetFromIdentity(&stack.Spec.RuntimeTargetIdentity)
		}
	}
	summary := EnvironmentSummary{Namespace: environment.Namespace, Name: environment.Name, UID: string(environment.UID), Generation: environment.Generation, NodeCount: environment.Spec.Capacity.NodeCount, Phase: phase, Ready: readyValue, ReadyStatus: readyStatus, Infrastructure: infraState, Runtime: runtimeState, Target: target, LastTransitionTime: lastTransition, CreatedAt: environment.CreationTimestamp, Deleting: !environment.DeletionTimestamp.IsZero()}
	if class != nil {
		summary.Class = class.Name
	}
	if len(runs) > 0 {
		summary.LatestTerraformRun = briefRun(runs[len(runs)-1])
	}
	if len(approvals) > 0 {
		approval := approvals[len(approvals)-1]
		summary.LatestApproval = approval.State
	}
	if len(runs) > 0 {
		latest := runs[len(runs)-1]
		if latest.Spec.Operation == "Plan" && latest.Status.HasChanges && latest.Status.ExecutionOutcome == "ChangesPresent" && summary.LatestApproval == "" {
			summary.LatestApproval = "Required"
		}
	}
	return summary
}

func runView(run *platformv1alpha1.TerraformRun, approvals []*platformv1alpha1.ChangeApproval) TerraformRunView {
	condition := findReady(run.Status.Conditions)
	state := "Pending"
	reason, message := "", ""
	if condition != nil {
		state = conditionState(*condition)
		reason, message = condition.Reason, condition.Message
	}
	view := TerraformRunView{Namespace: run.Namespace, Name: run.Name, UID: string(run.UID), Operation: run.Spec.Operation, PlanMode: run.Spec.PlanMode, State: state, Reason: reason, Message: message, Outcome: run.Status.ExecutionOutcome, HasChanges: run.Status.HasChanges, PlanDigest: run.Status.PlanDigest, PlanRef: run.Status.PlanRef, PlanReportRef: run.Status.PlanReportRef, PlanReportDigest: run.Status.PlanReportDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest, ExecutionContextDigest: run.Spec.ExecutionContextDigest, PlanExpiresAt: run.Status.PlanExpiresAt, PlanCreatedAt: run.Status.PlanCreatedAt, ArtifactsReady: run.Status.ArtifactsReady, EvidenceCaptured: run.Status.EvidenceCaptured, TerminalStartedAt: run.Status.TerminalStartedAt, TerminalFinishedAt: run.Status.TerminalFinishedAt, CreatedAt: run.CreationTimestamp}
	if len(approvals) > 0 {
		view.Approval = approvalPtr(approvalView(approvals[len(approvals)-1]))
	}
	return view
}

func approvalView(approval *platformv1alpha1.ChangeApproval) ApprovalView {
	state := "Pending"
	if approval.Status.ApprovedAt != nil {
		state = "Approved"
	} else if condition := findReady(approval.Status.Conditions); condition != nil {
		state = conditionState(*condition)
	}
	return ApprovalView{Namespace: approval.Namespace, Name: approval.Name, PlanRun: approval.Spec.PlanRunRef.Name, PlanRunUID: approval.Spec.PlanRunUID, State: state, PlanDigest: approval.Spec.PlanDigest, CreatedAt: approval.CreationTimestamp, ApprovedAt: approval.Status.ApprovedAt}
}
func matchingApprovals(approvals []platformv1alpha1.ChangeApproval, run *platformv1alpha1.TerraformRun) []*platformv1alpha1.ChangeApproval {
	result := make([]*platformv1alpha1.ChangeApproval, 0)
	for i := range approvals {
		item := &approvals[i]
		if item.Namespace == run.Namespace && item.Spec.PlanRunRef.Name == run.Name && item.Spec.PlanRunUID == string(run.UID) {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreationTimestamp.Before(&result[j].CreationTimestamp) })
	return result
}
func approvalMatchesRun(approval *platformv1alpha1.ChangeApproval, run *platformv1alpha1.TerraformRun) bool {
	return approval.Spec.PlanRunUID == string(run.UID) && approval.Spec.PlanRunRef.Name == run.Name && approval.Spec.PlanDigest == run.Status.PlanDigest && approval.Spec.ExecutionContextDigest == run.Spec.ExecutionContextDigest && approval.Spec.EffectivePlanInputDigest == run.Status.EffectivePlanInputDigest && approval.Spec.PlanReportRef == run.Status.PlanReportRef && approval.Spec.PlanReportDigest == run.Status.PlanReportDigest
}
func approvalRequestMatches(request ApprovalRequest, run *platformv1alpha1.TerraformRun) bool {
	return request.PlanRunUID == string(run.UID) && request.PlanDigest == run.Status.PlanDigest && request.ExecutionContextDigest == run.Spec.ExecutionContextDigest && request.EffectivePlanInputDigest == run.Status.EffectivePlanInputDigest && request.PlanReportRef == run.Status.PlanReportRef && request.PlanReportDigest == run.Status.PlanReportDigest
}

func buildTimeline(environment *platformv1alpha1.PlatformEnvironment, stack *platformv1alpha1.InfraStack, resourceSet *platformv1alpha1.ResourceSet, runs []*platformv1alpha1.TerraformRun, approvals []ApprovalView) []TimelineEvent {
	events := make([]TimelineEvent, 0)
	add := func(at metav1.Time, kind, resource, title, state, reason, message string) {
		if at.IsZero() {
			return
		}
		events = append(events, TimelineEvent{At: at, Kind: kind, Resource: resource, Title: title, State: state, Reason: reason, Message: message})
	}
	add(environment.CreationTimestamp, "PlatformEnvironment", environment.Namespace+"/"+environment.Name, "PlatformEnvironment created", "Created", "", "")
	addConditions := func(kind, resource string, conditions []metav1.Condition) {
		for _, condition := range conditions {
			add(condition.LastTransitionTime, kind, resource, condition.Type, string(condition.Status), condition.Reason, condition.Message)
		}
	}
	addConditions("PlatformEnvironment", environment.Namespace+"/"+environment.Name, environment.Status.Conditions)
	if stack != nil {
		add(stack.CreationTimestamp, "InfraStack", stack.Namespace+"/"+stack.Name, "InfraStack created", "Created", "", "")
		addConditions("InfraStack", stack.Namespace+"/"+stack.Name, stack.Status.Conditions)
	}
	for _, run := range runs {
		resource := run.Namespace + "/" + run.Name
		add(run.CreationTimestamp, "TerraformRun", resource, run.Spec.Operation+" run created", "Created", "", "")
		addConditions("TerraformRun", resource, run.Status.Conditions)
		if run.Status.TerminalStartedAt != nil {
			add(*run.Status.TerminalStartedAt, "TerraformRun", resource, run.Spec.Operation+" execution started", "Running", "", "")
		}
		if run.Status.TerminalFinishedAt != nil {
			add(*run.Status.TerminalFinishedAt, "TerraformRun", resource, run.Spec.Operation+" execution finished", run.Status.ExecutionOutcome, "", "")
		}
	}
	for _, approval := range approvals {
		resource := approval.Namespace + "/" + approval.Name
		add(approval.CreatedAt, "ChangeApproval", resource, "ChangeApproval created", approval.State, "", "")
		if approval.ApprovedAt != nil {
			add(*approval.ApprovedAt, "ChangeApproval", resource, "Plan approval recorded", "Approved", "", "")
		}
	}
	if resourceSet != nil {
		add(resourceSet.CreationTimestamp, "ResourceSet", resourceSet.Namespace+"/"+resourceSet.Name, "ResourceSet created", "Created", "", "")
		addConditions("ResourceSet", resourceSet.Namespace+"/"+resourceSet.Name, resourceSet.Status.Conditions)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].At.Equal(&events[j].At) {
			if events[i].Kind == events[j].Kind {
				return events[i].Resource < events[j].Resource
			}
			return events[i].Kind < events[j].Kind
		}
		return events[i].At.Before(&events[j].At)
	})
	return events
}

func conditionViews(conditions []metav1.Condition) []ConditionView {
	result := make([]ConditionView, 0, len(conditions))
	for _, condition := range conditions {
		result = append(result, *conditionView(&condition))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Type < result[j].Type })
	return result
}
func conditionView(condition *metav1.Condition) *ConditionView {
	if condition == nil {
		return nil
	}
	return &ConditionView{Type: condition.Type, Status: condition.Status, Reason: condition.Reason, Message: condition.Message, LastTransitionTime: condition.LastTransitionTime}
}
func findReady(conditions []metav1.Condition) *metav1.Condition {
	return meta.FindStatusCondition(conditions, "Ready")
}
func conditionState(condition metav1.Condition) string {
	if condition.Status == metav1.ConditionTrue {
		return "Ready"
	}
	if condition.Status == metav1.ConditionUnknown {
		return "Unknown"
	}
	if condition.Reason != "" {
		return condition.Reason
	}
	return "NotReady"
}
func targetFromIdentity(identity *platformv1alpha1.RuntimeTargetIdentity) TargetView {
	if identity == nil {
		return TargetView{}
	}
	return TargetView{Provider: identity.Provider, AccountID: identity.AccountID, Region: identity.Region, ClusterName: identity.ClusterName, IncarnationID: identity.IncarnationID}
}
func className(environment *platformv1alpha1.PlatformEnvironment) string {
	if environment.Spec.ClassRef != nil {
		return environment.Spec.ClassRef.Name
	}
	return ""
}
func findEnvironment(items []platformv1alpha1.PlatformEnvironment, namespace, name string) *platformv1alpha1.PlatformEnvironment {
	for i := range items {
		if items[i].Namespace == namespace && items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}
func findClass(items []platformv1alpha1.EnvironmentClass, name string) *platformv1alpha1.EnvironmentClass {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}
func findStack(items []platformv1alpha1.InfraStack, environment *platformv1alpha1.PlatformEnvironment) *platformv1alpha1.InfraStack {
	for i := range items {
		item := &items[i]
		if item.Namespace != environment.Namespace {
			continue
		}
		if (environment.Status.InfraStackRef != nil && item.Name == environment.Status.InfraStackRef.Name) || (environment.Spec.InfraStackRef.Name != "" && item.Name == environment.Spec.InfraStackRef.Name) || hasOwnerUID(item, environment.UID) {
			return item
		}
	}
	return nil
}
func findResourceSet(items []platformv1alpha1.ResourceSet, environment *platformv1alpha1.PlatformEnvironment) *platformv1alpha1.ResourceSet {
	for i := range items {
		item := &items[i]
		if item.Namespace != environment.Namespace {
			continue
		}
		if (environment.Status.ResourceSetRef != nil && item.Name == environment.Status.ResourceSetRef.Name) || hasOwnerUID(item, environment.UID) {
			return item
		}
	}
	return nil
}
func hasOwnerUID(object client.Object, uid types.UID) bool {
	if uid == "" {
		return false
	}
	for _, owner := range object.GetOwnerReferences() {
		if owner.UID == uid {
			return true
		}
	}
	return false
}
func briefRun(run *platformv1alpha1.TerraformRun) *RunBrief {
	condition := findReady(run.Status.Conditions)
	brief := &RunBrief{Name: run.Name, Operation: run.Spec.Operation, Outcome: run.Status.ExecutionOutcome, HasChanges: run.Status.HasChanges}
	if condition != nil {
		brief.Reason = condition.Reason
	}
	return brief
}
func approvalPtr(value ApprovalView) *ApprovalView { return &value }

func lastChangedAt(conditions []metav1.Condition, creation metav1.Time) *metav1.Time {
	latest := creation
	for _, condition := range conditions {
		if condition.LastTransitionTime.After(latest.Time) {
			latest = condition.LastTransitionTime
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}
func isReady(value string) bool { return strings.EqualFold(value, "ready") }
func maxTime(values ...metav1.Time) metav1.Time {
	var latest time.Time
	for _, value := range values {
		if value.Time.After(latest) {
			latest = value.Time
		}
	}
	return metav1.NewTime(latest)
}
