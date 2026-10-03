package console

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	platformv1alpha1 "github.com/miku-wwl/kube-platform-control-plane/api/v1alpha1"
	"github.com/miku-wwl/kube-platform-control-plane/internal/artifacts"
	"github.com/miku-wwl/kube-platform-control-plane/internal/planview"
)

type Server struct {
	client        client.Client
	artifactStore *artifacts.Store
	drafts        DraftGenerator
	provider      string
	logger        *slog.Logger
}

func NewServer(kubeClient client.Client, artifactStore *artifacts.Store, provider string, generator DraftGenerator, logger *slog.Logger) (*Server, error) {
	if kubeClient == nil {
		return nil, errors.New("Kubernetes client is required")
	}
	if generator == nil {
		return nil, errors.New("draft generator is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{client: kubeClient, artifactStore: artifactStore, drafts: generator, provider: provider, logger: logger}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/classes", s.listClasses)
	mux.HandleFunc("POST /api/classes", s.createClass)
	mux.HandleFunc("POST /api/classes/validate", s.validateClass)
	mux.HandleFunc("GET /api/classes/{name}", s.getClass)
	mux.HandleFunc("DELETE /api/classes/{name}", s.deleteClass)
	mux.HandleFunc("GET /api/class-templates", s.listClassTemplates)
	mux.HandleFunc("POST /api/class-templates", s.createClassTemplate)
	mux.HandleFunc("DELETE /api/class-templates/{name}", s.deleteClassTemplate)
	mux.HandleFunc("GET /api/environments", s.listEnvironments)
	mux.HandleFunc("POST /api/environments", s.createEnvironment)
	mux.HandleFunc("GET /api/environments/{namespace}/{name}", s.getEnvironment)
	mux.HandleFunc("PUT /api/environments/{namespace}/{name}", s.updateEnvironment)
	mux.HandleFunc("DELETE /api/environments/{namespace}/{name}", s.deleteEnvironment)
	mux.HandleFunc("GET /api/environments/{namespace}/{name}/timeline", s.getTimeline)
	mux.HandleFunc("GET /api/environments/{namespace}/{name}/terraform-runs", s.getTerraformRuns)
	mux.HandleFunc("GET /api/environments/{namespace}/{name}/architecture", s.getArchitecture)
	mux.HandleFunc("GET /api/terraform-runs/{namespace}/{name}", s.getTerraformRun)
	mux.HandleFunc("GET /api/terraform-runs/{namespace}/{name}/plan", s.getPlan)
	mux.HandleFunc("POST /api/terraform-runs/{namespace}/{name}/approve", s.approvePlan)
	mux.HandleFunc("POST /api/drafts", s.generateDraft)
	return requestLog(s.logger, mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "aiProvider": s.provider, "artifactPreview": s.artifactStore != nil})
}

type CreateEnvironmentRequest struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	ClassRef       string `json:"classRef"`
	Region         string `json:"region,omitempty"`
	NodeCount      int32  `json:"nodeCount"`
	ValkeyEnabled  bool   `json:"valkeyEnabled,omitempty"`
	ValkeyShards   int32  `json:"valkeyShards,omitempty"`
	ValkeyReplicas int32  `json:"valkeyReplicas,omitempty"`
}

type UpdateEnvironmentRequest struct {
	UID        string `json:"uid"`
	Generation int64  `json:"generation"`
	NodeCount  int32  `json:"nodeCount"`
}

type DeleteEnvironmentRequest struct {
	UID string `json:"uid"`
}

type ApprovalRequest struct {
	PlanRunUID               string `json:"planRunUID"`
	PlanDigest               string `json:"planDigest"`
	ExecutionContextDigest   string `json:"executionContextDigest"`
	EffectivePlanInputDigest string `json:"effectivePlanInputDigest"`
	PlanReportRef            string `json:"planReportRef"`
	PlanReportDigest         string `json:"planReportDigest"`
}

func (s *Server) listClasses(w http.ResponseWriter, r *http.Request) {
	var classes platformv1alpha1.EnvironmentClassList
	if err := s.client.List(r.Context(), &classes); err != nil {
		writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
		return
	}
	items := make([]ClassView, 0, len(classes.Items))
	usage, err := s.classUsage(r.Context())
	if err != nil {
		s.writeObjectError(w, err, "PlatformEnvironment")
		return
	}
	for i := range classes.Items {
		view := classView(&classes.Items[i])
		view.UsageCount = usage[view.Name]
		items = append(items, view)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.loadSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
		return
	}
	items := make([]EnvironmentSummary, 0, len(snapshot.environments.Items))
	for i := range snapshot.environments.Items {
		items = append(items, snapshot.detail(&snapshot.environments.Items[i]).EnvironmentSummary)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace == items[j].Namespace {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getEnvironment(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.loadSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
		return
	}
	environment := findEnvironment(snapshot.environments.Items, r.PathValue("namespace"), r.PathValue("name"))
	if environment == nil {
		writeError(w, http.StatusNotFound, "NotFound", "PlatformEnvironment was not found")
		return
	}
	writeJSON(w, http.StatusOK, snapshot.detail(environment))
}

func (s *Server) getTimeline(w http.ResponseWriter, r *http.Request) {
	detail, status, err := s.environmentDetail(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, status, "ReadFailed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail.Timeline)
}

func (s *Server) getTerraformRuns(w http.ResponseWriter, r *http.Request) {
	detail, status, err := s.environmentDetail(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, status, "ReadFailed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail.TerraformRuns)
}

func (s *Server) getTerraformRun(w http.ResponseWriter, r *http.Request) {
	var run platformv1alpha1.TerraformRun
	if err := s.client.Get(r.Context(), client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}, &run); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "NotFound", "TerraformRun was not found")
		} else {
			writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
		}
		return
	}
	var approvals platformv1alpha1.ChangeApprovalList
	if err := s.client.List(r.Context(), &approvals, client.InNamespace(run.Namespace)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runView(&run, matchingApprovals(approvals.Items, &run)))
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	var run platformv1alpha1.TerraformRun
	if err := s.client.Get(r.Context(), client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}, &run); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "NotFound", "TerraformRun was not found")
		} else {
			writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
		}
		return
	}
	runSummary := runView(&run, nil)
	view := PlanView{TerraformRun: &runSummary, Available: false, Message: "Terraform plan visualization is not available for this run."}
	if run.Spec.Operation != "Plan" || run.Status.PlanDigest == "" || run.Status.PlanReportRef == "" {
		writeJSON(w, http.StatusOK, view)
		return
	}
	if s.artifactStore == nil {
		view.Message = "Artifact storage is not configured for the local console."
		writeJSON(w, http.StatusOK, view)
		return
	}
	key := path.Join(path.Dir(run.Status.PlanReportRef), "plan-visualization.json")
	_, content, err := s.artifactStore.GetVerifiedByKey(r.Context(), key)
	if err != nil {
		view.Message = "This plan predates optional architecture evidence, or that evidence is unavailable."
		writeJSON(w, http.StatusOK, view)
		return
	}
	var graph planview.Document
	if err := json.Unmarshal(content, &graph); err != nil {
		writeError(w, http.StatusBadGateway, "InvalidPlanEvidence", "Stored plan visualization is not valid JSON")
		return
	}
	if graph.RunUID != string(run.UID) || graph.PlanDigest != run.Status.PlanDigest {
		writeError(w, http.StatusConflict, "StalePlanEvidence", "Plan visualization does not match the current TerraformRun identity and digest")
		return
	}
	view.Available, view.Message, view.Graph = true, "Derived from the immutable Terraform plan; informational only.", &graph
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) getArchitecture(w http.ResponseWriter, r *http.Request) {
	detail, status, err := s.environmentDetail(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeError(w, status, "ReadFailed", err.Error())
		return
	}
	if detail.InfrastructureDetail == nil || detail.InfrastructureDetail.LatestPlanRun == "" {
		writeJSON(w, http.StatusOK, PlanView{Available: false, Message: "No Terraform Plan is associated with this environment yet."})
		return
	}
	httpRequest := *r
	httpRequest.SetPathValue("namespace", detail.Namespace)
	httpRequest.SetPathValue("name", detail.InfrastructureDetail.LatestPlanRun)
	s.getPlan(w, &httpRequest)
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var request CreateEnvironmentRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	environment, class, err := s.validateEnvironmentRequest(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "InvalidDraft", err.Error())
		return
	}
	if err := s.client.Create(r.Context(), environment); err != nil {
		if apierrors.IsAlreadyExists(err) {
			writeError(w, http.StatusConflict, "AlreadyExists", "a PlatformEnvironment with this namespace/name already exists")
		} else {
			writeError(w, http.StatusBadRequest, "CreateRejected", err.Error())
		}
		return
	}
	s.logger.Info("PlatformEnvironment submitted", "namespace", environment.Namespace, "name", environment.Name, "class", class.Name, "generation", environment.Generation)
	writeJSON(w, http.StatusCreated, map[string]any{"namespace": environment.Namespace, "name": environment.Name, "uid": string(environment.UID), "generation": environment.Generation, "message": "PlatformEnvironment created; the existing control plane owns reconciliation."})
}

func (s *Server) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	var request UpdateEnvironmentRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	key := client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
	var environment platformv1alpha1.PlatformEnvironment
	if err := s.client.Get(r.Context(), key, &environment); err != nil {
		s.writeObjectError(w, err, "PlatformEnvironment")
		return
	}
	if request.UID == "" || request.UID != string(environment.UID) || request.Generation != environment.Generation {
		writeError(w, http.StatusConflict, "StaleEnvironment", "environment identity or generation changed; reload before updating")
		return
	}
	if environment.Spec.ClassRef == nil || environment.Spec.ClassRef.Name == "" {
		writeError(w, http.StatusConflict, "UnsupportedMode", "only class-based environments can be updated through this console")
		return
	}
	if environment.Spec.DesiredState == platformv1alpha1.DesiredStateDestroy || !environment.DeletionTimestamp.IsZero() {
		writeError(w, http.StatusConflict, "EnvironmentDeleting", "deleting environments cannot be updated")
		return
	}
	var class platformv1alpha1.EnvironmentClass
	if err := s.client.Get(r.Context(), client.ObjectKey{Name: environment.Spec.ClassRef.Name}, &class); err != nil {
		s.writeObjectError(w, err, "EnvironmentClass")
		return
	}
	if err := validateNodeCount(request.NodeCount, class.Spec.CapacityBounds); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "InvalidCapacity", err.Error())
		return
	}
	environment.Spec.Capacity.NodeCount = request.NodeCount
	if err := s.client.Update(r.Context(), &environment); err != nil {
		writeError(w, http.StatusConflict, "UpdateRejected", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespace": environment.Namespace, "name": environment.Name, "generation": environment.Generation, "message": "desired capacity updated; the existing control plane will reconcile it."})
}

func (s *Server) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	var request DeleteEnvironmentRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	key := client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
	var environment platformv1alpha1.PlatformEnvironment
	if err := s.client.Get(r.Context(), key, &environment); err != nil {
		s.writeObjectError(w, err, "PlatformEnvironment")
		return
	}
	if request.UID == "" || request.UID != string(environment.UID) {
		writeError(w, http.StatusConflict, "StaleEnvironment", "environment identity changed; reload before deleting")
		return
	}
	uid := types.UID(request.UID)
	if err := s.client.Delete(r.Context(), &environment, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
		writeError(w, http.StatusConflict, "DeleteRejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"namespace": environment.Namespace, "name": environment.Name, "message": "deletion requested; existing finalizers own runtime prune and Terraform destroy."})
}

func (s *Server) approvePlan(w http.ResponseWriter, r *http.Request) {
	var request ApprovalRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	var run platformv1alpha1.TerraformRun
	if err := s.client.Get(r.Context(), client.ObjectKey{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}, &run); err != nil {
		s.writeObjectError(w, err, "TerraformRun")
		return
	}
	if run.Spec.Operation != "Plan" || run.Status.ExecutionOutcome != "ChangesPresent" || !run.Status.HasChanges || !run.Status.ArtifactsReady || !run.Status.EvidenceCaptured || !meta.IsStatusConditionTrue(run.Status.Conditions, "Ready") {
		writeError(w, http.StatusConflict, "PlanNotApprovable", "only a completed, successful Plan with changes and durable evidence can be approved")
		return
	}
	if run.Status.PlanExpiresAt == nil || !time.Now().Before(run.Status.PlanExpiresAt.Time) {
		writeError(w, http.StatusConflict, "PlanExpired", "the saved plan has expired and must be regenerated")
		return
	}
	if run.UID == "" || run.Status.PlanRef == "" || run.Status.PlanDigest == "" || run.Spec.ExecutionContextDigest == "" || run.Status.EffectivePlanInputDigest == "" || run.Status.PlanReportRef == "" || run.Status.PlanReportDigest == "" || run.Status.SourceBundleRef == "" || run.Status.SourceBundleDigest == "" || run.Status.TerminalResultRef == "" || run.Status.TerminalResultDigest == "" || run.Spec.RuntimeTargetIdentityDigest == "" || run.Spec.InfrastructureExecutionIdentityDigest == "" {
		writeError(w, http.StatusConflict, "PlanEvidenceIncomplete", "the exact plan binding evidence is incomplete")
		return
	}
	if !approvalRequestMatches(request, &run) {
		writeError(w, http.StatusConflict, "StalePlan", "submitted approval binding does not exactly match the current PlanRun")
		return
	}
	var stack platformv1alpha1.InfraStack
	if err := s.client.Get(r.Context(), client.ObjectKey{Namespace: run.Namespace, Name: run.Spec.StackRef.Name}, &stack); err != nil {
		s.writeObjectError(w, err, "InfraStack")
		return
	}
	if stack.Status.LatestPlanRunRef == nil || stack.Status.LatestPlanRunRef.Name != run.Name || stack.Generation != run.Spec.InfraStackGeneration {
		writeError(w, http.StatusConflict, "PlanNotCurrent", "the plan is not the latest plan for the current InfraStack generation")
		return
	}
	if run.Spec.InfrastructureExecutionRoleARN != stack.Spec.InfrastructureExecutionIdentity.RoleARN || run.Spec.RunnerServiceAccountName != stack.Spec.RunnerServiceAccountName || run.Spec.RunnerManagementRoleARN != stack.Spec.RunnerManagementRoleARN || run.Spec.RunnerServiceAccountIdentityDigest != stack.Spec.RunnerServiceAccountIdentityDigest {
		writeError(w, http.StatusConflict, "ExecutionIdentityMismatch", "the PlanRun execution identity does not match the current InfraStack")
		return
	}
	if stack.Spec.MutationFence && run.Spec.PlanMode != "Destroy" {
		writeError(w, http.StatusConflict, "MutationFenced", "the InfraStack mutation fence blocks non-destroy approval")
		return
	}
	if !meta.IsStatusConditionTrue(stack.Status.Conditions, "Ready") {
		ready := meta.FindStatusCondition(stack.Status.Conditions, "Ready")
		if ready == nil || ready.Reason != "WaitingApproval" {
			writeError(w, http.StatusConflict, "StackNotWaitingApproval", "the InfraStack is not waiting for approval of this plan")
			return
		}
	}
	var existing platformv1alpha1.ChangeApprovalList
	if err := s.client.List(r.Context(), &existing, client.InNamespace(run.Namespace)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
		return
	}
	for i := range existing.Items {
		approval := &existing.Items[i]
		if approval.Spec.PlanRunUID == string(run.UID) && approval.Spec.PlanRunRef.Name == run.Name {
			if approvalMatchesRun(approval, &run) {
				writeJSON(w, http.StatusOK, approvalView(approval))
				return
			}
			writeError(w, http.StatusConflict, "ApprovalNameConflict", "an approval exists for this PlanRun but its immutable binding does not match")
			return
		}
	}
	name := approvalName(run.Name, string(run.UID))
	approval := &platformv1alpha1.ChangeApproval{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: run.Namespace, Labels: map[string]string{"platform.example.io/managed-by": "platform-api"}},
		Spec: platformv1alpha1.ChangeApprovalSpec{
			PlanRunRef: corev1.LocalObjectReference{Name: run.Name}, PlanRunUID: string(run.UID), PlanDigest: run.Status.PlanDigest,
			ExecutionContextDigest: run.Spec.ExecutionContextDigest, EffectivePlanInputDigest: run.Status.EffectivePlanInputDigest,
			PlanReportRef: run.Status.PlanReportRef, PlanReportDigest: run.Status.PlanReportDigest,
		},
	}
	if err := s.client.Create(r.Context(), approval); err != nil {
		if apierrors.IsAlreadyExists(err) {
			writeError(w, http.StatusConflict, "ApprovalNameConflict", "an approval object with the generated name already exists; no approval was changed")
		} else {
			writeError(w, http.StatusBadRequest, "ApprovalRejected", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, approvalView(approval))
}

func (s *Server) generateDraft(w http.ResponseWriter, r *http.Request) {
	var request DraftRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Description) == "" || len(request.Description) > 4000 {
		writeError(w, http.StatusBadRequest, "InvalidRequest", "description must contain between 1 and 4000 characters")
		return
	}
	if request.Namespace == "" {
		request.Namespace = "default"
	}
	var class platformv1alpha1.EnvironmentClass
	if err := s.client.Get(r.Context(), client.ObjectKey{Name: request.ClassRef}, &class); err != nil {
		s.writeObjectError(w, err, "EnvironmentClass")
		return
	}
	intent, err := s.drafts.Generate(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "DraftGenerationFailed", err.Error())
		return
	}
	if intent.ClassRef == "" {
		intent.ClassRef = request.ClassRef
	}
	if intent.Namespace == "" {
		intent.Namespace = request.Namespace
	}
	if intent.Region == "" {
		intent.Region = request.Region
	}
	if intent.Name == "" {
		intent.Name = request.Name
	}
	if intent.NodeCount == 0 {
		intent.NodeCount = request.NodeCount
	}
	if request.ClassRef != "" && intent.ClassRef != request.ClassRef {
		writeError(w, http.StatusUnprocessableEntity, "DraftRejected", "generated classRef differs from the explicitly selected EnvironmentClass")
		return
	}
	if request.Namespace != "" && intent.Namespace != request.Namespace {
		writeError(w, http.StatusUnprocessableEntity, "DraftRejected", "generated namespace differs from the explicitly selected namespace")
		return
	}
	if intent.ValkeyShards == 0 && intent.ValkeyEnabled {
		intent.ValkeyShards = 1
	}
	normalized := CreateEnvironmentRequest{Name: intent.Name, Namespace: intent.Namespace, ClassRef: intent.ClassRef, Region: intent.Region, NodeCount: intent.NodeCount, ValkeyEnabled: intent.ValkeyEnabled, ValkeyShards: intent.ValkeyShards, ValkeyReplicas: intent.ValkeyReplicas}
	environment, _, err := s.validateEnvironmentRequest(r.Context(), normalized)
	if err != nil {
		writeJSON(w, http.StatusOK, DraftResponse{Provider: s.provider, Intent: intent, Valid: false, Errors: []string{err.Error()}})
		return
	}
	yaml, err := environmentYAML(environment)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DraftRenderFailed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, DraftResponse{Provider: s.provider, Intent: intent, Valid: true, Warnings: []string{"AI output is advisory. Review the fields; creation requires a separate explicit action."}, YAML: yaml})
}

func (s *Server) validateEnvironmentRequest(ctx context.Context, request CreateEnvironmentRequest) (*platformv1alpha1.PlatformEnvironment, *platformv1alpha1.EnvironmentClass, error) {
	if problems := validation.IsDNS1123Subdomain(request.Name); len(problems) > 0 {
		return nil, nil, fmt.Errorf("invalid environment name: %s", strings.Join(problems, "; "))
	}
	if problems := validation.IsDNS1123Label(request.Namespace); len(problems) > 0 {
		return nil, nil, fmt.Errorf("invalid namespace: %s", strings.Join(problems, "; "))
	}
	if request.ClassRef == "" {
		return nil, nil, errors.New("classRef is required")
	}
	if problems := validation.IsDNS1123Subdomain(request.ClassRef); len(problems) > 0 {
		return nil, nil, errors.New("classRef is not a valid EnvironmentClass name")
	}
	var class platformv1alpha1.EnvironmentClass
	if err := s.client.Get(ctx, client.ObjectKey{Name: request.ClassRef}, &class); err != nil {
		return nil, nil, fmt.Errorf("EnvironmentClass %q is unavailable: %w", request.ClassRef, err)
	}
	if class.Generation > 0 && class.Status.ObservedGeneration != class.Generation {
		return nil, nil, errors.New("EnvironmentClass has not reconciled its current generation")
	}
	if !class.DeletionTimestamp.IsZero() {
		return nil, nil, errors.New("EnvironmentClass is being deleted")
	}
	if !meta.IsStatusConditionTrue(class.Status.Conditions, "Ready") {
		return nil, nil, errors.New("EnvironmentClass is not Ready")
	}
	if class.Spec.Target.Provider == "" || class.Spec.Target.ClusterName == "" {
		return nil, nil, errors.New("EnvironmentClass target identity is incomplete")
	}
	if request.Region == "" {
		request.Region = class.Spec.Target.Region
	}
	if len(class.Spec.AllowedRegions) > 0 && !contains(class.Spec.AllowedRegions, request.Region) {
		return nil, nil, fmt.Errorf("region %q is not allowed by EnvironmentClass", request.Region)
	}
	if err := validateNodeCount(request.NodeCount, class.Spec.CapacityBounds); err != nil {
		return nil, nil, err
	}
	if request.ValkeyEnabled && request.ValkeyShards < 1 {
		return nil, nil, errors.New("valkeyShards must be at least 1 when Valkey is enabled")
	}
	if request.ValkeyShards < 0 || request.ValkeyReplicas < 0 {
		return nil, nil, errors.New("Valkey shard and replica counts cannot be negative")
	}
	var namespace corev1.Namespace
	if err := s.client.Get(ctx, client.ObjectKey{Name: request.Namespace}, &namespace); err != nil {
		return nil, nil, fmt.Errorf("namespace %q is unavailable: %w", request.Namespace, err)
	}
	var existing platformv1alpha1.PlatformEnvironmentList
	if err := s.client.List(ctx, &existing); err != nil {
		return nil, nil, fmt.Errorf("list existing environments: %w", err)
	}
	usage := int32(0)
	for i := range existing.Items {
		item := &existing.Items[i]
		if item.Spec.ClassRef != nil && item.Spec.ClassRef.Name == class.Name {
			usage++
		}
	}
	if limit := class.Spec.CapacityBounds.MaxEnvironments; limit > 0 && usage >= limit {
		return nil, nil, fmt.Errorf("EnvironmentClass environment limit %d has been reached", limit)
	}
	environment := &platformv1alpha1.PlatformEnvironment{TypeMeta: metav1.TypeMeta{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PlatformEnvironment"}, ObjectMeta: metav1.ObjectMeta{Name: request.Name, Namespace: request.Namespace}, Spec: platformv1alpha1.PlatformEnvironmentSpec{ClassRef: &corev1.LocalObjectReference{Name: request.ClassRef}, Region: request.Region, Capacity: platformv1alpha1.CapacitySpec{NodeCount: request.NodeCount}, Valkey: platformv1alpha1.ValkeyIntentSpec{Enabled: request.ValkeyEnabled, Shards: request.ValkeyShards, Replicas: request.ValkeyReplicas}, DesiredState: platformv1alpha1.DesiredStatePresent}}
	return environment, &class, nil
}

func (s *Server) environmentDetail(ctx context.Context, namespace, name string) (EnvironmentDetail, int, error) {
	snapshot, err := s.loadSnapshot(ctx)
	if err != nil {
		return EnvironmentDetail{}, http.StatusServiceUnavailable, err
	}
	environment := findEnvironment(snapshot.environments.Items, namespace, name)
	if environment == nil {
		return EnvironmentDetail{}, http.StatusNotFound, errors.New("PlatformEnvironment was not found")
	}
	return snapshot.detail(environment), http.StatusOK, nil
}

func (s *Server) writeObjectError(w http.ResponseWriter, err error, kind string) {
	if apierrors.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "NotFound", kind+" was not found")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "KubernetesReadFailed", err.Error())
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidJSON", err.Error())
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "InvalidJSON", "request must contain exactly one JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("platform API request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started).Round(time.Millisecond))
	})
}

func validateArtifactEndpoint(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "http" || parsed.User != nil || !(host == "localhost" || host == "127.0.0.1" || host == "::1") {
		return fmt.Errorf("artifact endpoint must be a local HTTP endpoint; refusing %q", raw)
	}
	return nil
}

func NewArtifactStoreFromEnvironment() (*artifacts.Store, error) {
	endpoint := os.Getenv("PCP_ARTIFACT_ENDPOINT")
	bucket := os.Getenv("PCP_ARTIFACT_BUCKET")
	if endpoint == "" || bucket == "" {
		return nil, nil
	}
	if err := validateArtifactEndpoint(endpoint); err != nil {
		return nil, err
	}
	region := os.Getenv("PCP_ARTIFACT_REGION")
	if region == "" {
		region = "us-east-1"
	}
	return artifacts.NewS3StoreWithKMS(endpoint, region, bucket, os.Getenv("PCP_ARTIFACT_KMS_KEY_ID"))
}

func validateNodeCount(count int32, bounds platformv1alpha1.CapacityBounds) error {
	if count < 0 {
		return errors.New("nodeCount cannot be negative")
	}
	if bounds.MinNodeCount > 0 && count < bounds.MinNodeCount {
		return fmt.Errorf("nodeCount %d is below the class minimum %d", count, bounds.MinNodeCount)
	}
	if bounds.MaxNodeCount > 0 && count > bounds.MaxNodeCount {
		return fmt.Errorf("nodeCount %d exceeds the class maximum %d", count, bounds.MaxNodeCount)
	}
	return nil
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func approvalName(planName, planUID string) string {
	digest := sha256.Sum256([]byte(planName + "\x00" + planUID))
	return "approve-" + hex.EncodeToString(digest[:])[:24]
}

func environmentYAML(environment *platformv1alpha1.PlatformEnvironment) (string, error) {
	manifest := struct {
		APIVersion string `json:"apiVersion" yaml:"apiVersion"`
		Kind       string `json:"kind" yaml:"kind"`
		Metadata   struct {
			Name      string `json:"name" yaml:"name"`
			Namespace string `json:"namespace" yaml:"namespace"`
		} `json:"metadata" yaml:"metadata"`
		Spec platformv1alpha1.PlatformEnvironmentSpec `json:"spec" yaml:"spec"`
	}{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PlatformEnvironment", Spec: environment.Spec}
	manifest.Metadata.Name = environment.Name
	manifest.Metadata.Namespace = environment.Namespace
	content, err := yaml.Marshal(manifest)
	return string(content), err
}
