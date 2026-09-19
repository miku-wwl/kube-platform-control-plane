package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=penv
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type PlatformEnvironment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformEnvironmentSpec   `json:"spec,omitempty"`
	Status PlatformEnvironmentStatus `json:"status,omitempty"`
}

type PlatformEnvironmentSpec struct {
	ClassRef      *corev1.LocalObjectReference `json:"classRef,omitempty"`
	InfraStackRef corev1.LocalObjectReference  `json:"infraStackRef,omitempty"`
	Target        TargetReference              `json:"target,omitempty"`
	Region        string                       `json:"region,omitempty"`
	Capacity      CapacitySpec                 `json:"capacity,omitempty"`
	Valkey        ValkeyIntentSpec             `json:"valkey,omitempty"`
	DesiredState  DesiredState                 `json:"desiredState,omitempty"`
}

type PlatformEnvironmentStatus struct {
	ObservedGeneration    int64                        `json:"observedGeneration,omitempty"`
	Conditions            []metav1.Condition           `json:"conditions,omitempty"`
	InfraStackRef         *corev1.LocalObjectReference `json:"infraStackRef,omitempty"`
	ResourceSetRef        *corev1.LocalObjectReference `json:"resourceSetRef,omitempty"`
	TargetIdentity        *RuntimeTargetIdentity       `json:"targetIdentity,omitempty"`
	TargetConnection      *TargetConnectionProfile     `json:"targetConnection,omitempty"`
	CleanupEvidenceRef    string                       `json:"cleanupEvidenceRef,omitempty"`
	CleanupEvidenceDigest string                       `json:"cleanupEvidenceDigest,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=eclass
// +kubebuilder:subresource:status
type EnvironmentClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EnvironmentClassSpec   `json:"spec,omitempty"`
	Status EnvironmentClassStatus `json:"status,omitempty"`
}

type EnvironmentClassSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass spec is immutable"
	Version        string             `json:"version"`
	Source         SourceSpec         `json:"source"`
	Backend        BackendSpec        `json:"backend"`
	Executor       ExecutorSpec       `json:"executor"`
	RunnerProfile  RunnerProfileSpec  `json:"runnerProfile"`
	RuntimeProfile RuntimeProfileSpec `json:"runtimeProfile"`
	Target         TargetReference    `json:"target"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass spec is immutable"
	AllowedRegions []string `json:"allowedRegions,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass spec is immutable"
	CapacityBounds CapacityBounds `json:"capacityBounds,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass spec is immutable"
	ApprovalPolicy ApprovalPolicy `json:"approvalPolicy,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass spec is immutable"
	DataRetentionPolicy string `json:"dataRetentionPolicy,omitempty"`
}

type EnvironmentClassStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	UsageCount         int32              `json:"usageCount,omitempty"`
	SpecDigest         string             `json:"specDigest,omitempty"`
}

type CapacitySpec struct {
	NodeCount int32 `json:"nodeCount,omitempty"`
}

type CapacityBounds struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass capacity bounds are immutable"
	MinNodeCount int32 `json:"minNodeCount,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass capacity bounds are immutable"
	MaxNodeCount int32 `json:"maxNodeCount,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass capacity bounds are immutable"
	MaxEnvironments int32 `json:"maxEnvironments,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass capacity bounds are immutable"
	MaxConcurrentPlans int32 `json:"maxConcurrentPlans,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass capacity bounds are immutable"
	MaxConcurrentApplies int32 `json:"maxConcurrentApplies,omitempty"`
}

type ValkeyIntentSpec struct {
	Enabled  bool  `json:"enabled,omitempty"`
	Shards   int32 `json:"shards,omitempty"`
	Replicas int32 `json:"replicas,omitempty"`
}

type RunnerProfileSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runner profile is immutable"
	ServiceAccountName string `json:"serviceAccountName"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runner profile is immutable"
	Image string `json:"image"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runner profile is immutable"
	ImageDigest string `json:"imageDigest"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runner profile is immutable"
	TerraformVersion string `json:"terraformVersion"`
}

type RuntimeProfileSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runtime profile is immutable"
	OperatorImage string `json:"operatorImage,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runtime profile is immutable"
	OperatorImageDigest string `json:"operatorImageDigest,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runtime profile is immutable"
	OperatorManifestRef string `json:"operatorManifestRef,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runtime profile is immutable"
	ValkeyImage string `json:"valkeyImage,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runtime profile is immutable"
	NetworkPolicyProfile string `json:"networkPolicyProfile,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass runtime profile is immutable"
	RuntimeObjects []RuntimeObject `json:"runtimeObjects,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=istack
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Desired",type="string",JSONPath=".spec.desiredState"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type InfraStack struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InfraStackSpec   `json:"spec,omitempty"`
	Status InfraStackStatus `json:"status,omitempty"`
}

type InfraStackSpec struct {
	Source         SourceSpec     `json:"source"`
	Backend        BackendSpec    `json:"backend"`
	Workspace      string         `json:"workspace"`
	Capacity       CapacitySpec   `json:"capacity,omitempty"`
	Variables      VariablesSpec  `json:"variables,omitempty"`
	Executor       ExecutorSpec   `json:"executor"`
	DesiredState   DesiredState   `json:"desiredState,omitempty"`
	ApprovalPolicy ApprovalPolicy `json:"approvalPolicy,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="InfraStack ownerEnvironmentUID is immutable"
	OwnerEnvironmentUID             string                          `json:"ownerEnvironmentUID,omitempty"`
	InfrastructureExecutionIdentity InfrastructureExecutionIdentity `json:"infrastructureExecutionIdentity,omitempty"`
	RuntimeTargetIdentity           RuntimeTargetIdentity           `json:"runtimeTargetIdentity,omitempty"`
	TargetConnectionProfile         TargetConnectionProfile         `json:"targetConnectionProfile,omitempty"`
	MutationFence                   bool                            `json:"mutationFence,omitempty"`
	RuntimeMutationAllowed          bool                            `json:"runtimeMutationAllowed,omitempty"`
	RunnerServiceAccountName        string                          `json:"runnerServiceAccountName,omitempty"`
	ConcurrencyGroup                string                          `json:"concurrencyGroup,omitempty"`
	MaxConcurrentPlans              int32                           `json:"maxConcurrentPlans,omitempty"`
	MaxConcurrentApplies            int32                           `json:"maxConcurrentApplies,omitempty"`
}

type SourceSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass source is immutable"
	URL string `json:"url"`
	// +kubebuilder:validation:Pattern=`^[0-9a-fA-F]{40,64}$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass source is immutable"
	Revision string `json:"revision"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass source is immutable"
	Path string `json:"path"`
}

type BackendSpec struct {
	// +kubebuilder:validation:Enum=s3
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass backend is immutable"
	Type string `json:"type"`
	// +kubebuilder:validation:XValidation:rule="self.name == oldSelf.name",message="EnvironmentClass backend config reference is immutable"
	ConfigRef corev1.LocalObjectReference `json:"configRef"`
	// +kubebuilder:validation:XValidation:rule="self.serviceAccountName == oldSelf.serviceAccountName",message="EnvironmentClass backend auth reference is immutable"
	AuthRef ServiceAccountReference `json:"authRef"`
	// +kubebuilder:validation:XValidation:rule="duration(self) == duration(oldSelf)",message="EnvironmentClass backend is immutable"
	LockTimeout metav1.Duration `json:"lockTimeout,omitempty"`
}

type ServiceAccountReference struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass backend auth reference is immutable"
	ServiceAccountName string `json:"serviceAccountName"`
}

type VariablesSpec struct {
	SecretRefs      []corev1.LocalObjectReference `json:"secretRefs,omitempty"`
	SecretVariables []SecretVariableReference     `json:"secretVariables,omitempty"`
}

type SecretVariableReference struct {
	Variable     string                   `json:"variable"`
	SecretKeyRef corev1.SecretKeySelector `json:"secretKeyRef"`
}

type ExecutorSpec struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass executor is immutable"
	TerraformVersion string `json:"terraformVersion"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass executor is immutable"
	Image string `json:"image"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass executor is immutable"
	WorkDir string `json:"workDir"`
	// +kubebuilder:validation:XValidation:rule="duration(self) == duration(oldSelf)",message="EnvironmentClass executor is immutable"
	ExecutionTimeout metav1.Duration `json:"executionTimeout"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass executor is immutable"
	Parallelism *int32 `json:"parallelism,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass executor is immutable"
	RuntimeOS string `json:"runtimeOS,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass executor is immutable"
	RuntimeArchitecture string `json:"runtimeArchitecture,omitempty"`
}

type TargetReference struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	Provider string `json:"provider"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	Account string `json:"account,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	Region string `json:"region,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	ClusterName string `json:"clusterName"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	ClusterID string `json:"clusterID,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	ClusterARN string `json:"clusterARN,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	IncarnationID string `json:"incarnationID,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EnvironmentClass target is immutable"
	ConnectionProfileRef string `json:"connectionProfileRef,omitempty"`
}

type InfrastructureExecutionIdentity struct {
	Provider       string `json:"provider"`
	Mode           string `json:"mode"`
	AccountID      string `json:"accountID"`
	Region         string `json:"region"`
	RoleARN        string `json:"roleARN,omitempty"`
	SessionProfile string `json:"sessionProfile,omitempty"`
}

type RuntimeTargetIdentity struct {
	Provider      string `json:"provider"`
	AccountID     string `json:"accountID"`
	Region        string `json:"region"`
	ClusterARN    string `json:"clusterARN,omitempty"`
	ClusterName   string `json:"clusterName,omitempty"`
	IncarnationID string `json:"incarnationID"`
}

type TargetConnectionProfile struct {
	Endpoint            string `json:"endpoint"`
	CACertificateDigest string `json:"caCertificateDigest,omitempty"`
	AuthMode            string `json:"authMode"`
	NetworkRouteProfile string `json:"networkRouteProfile,omitempty"`
	KubeContext         string `json:"kubeContext,omitempty"`
}

// +kubebuilder:validation:Enum=Present;Destroy
type DesiredState string

const (
	DesiredStatePresent DesiredState = "Present"
	DesiredStateDestroy DesiredState = "Destroy"
)

// +kubebuilder:validation:Enum=Manual;Automatic
type ApprovalPolicy string

const (
	ApprovalPolicyManual    ApprovalPolicy = "Manual"
	ApprovalPolicyAutomatic ApprovalPolicy = "Automatic"
)

type InfraStackStatus struct {
	ObservedGeneration            int64                        `json:"observedGeneration,omitempty"`
	Conditions                    []metav1.Condition           `json:"conditions,omitempty"`
	LatestPlanRunRef              *corev1.LocalObjectReference `json:"latestPlanRunRef,omitempty"`
	LastAppliedRunRef             *corev1.LocalObjectReference `json:"lastAppliedRunRef,omitempty"`
	LastAppliedSourceBundleRef    string                       `json:"lastAppliedSourceBundleRef,omitempty"`
	LastAppliedSourceBundleDigest string                       `json:"lastAppliedSourceBundleDigest,omitempty"`
	LastConverged                 *LastConvergedSourceClosure  `json:"lastConverged,omitempty"`
	ConvergenceEvidenceRef        string                       `json:"convergenceEvidenceRef,omitempty"`
	ConvergenceEvidenceDigest     string                       `json:"convergenceEvidenceDigest,omitempty"`
	TargetDiscoveryRef            string                       `json:"targetDiscoveryRef,omitempty"`
	TargetDiscoveryDigest         string                       `json:"targetDiscoveryDigest,omitempty"`
	DriftReportRef                string                       `json:"driftReportRef,omitempty"`
	DriftReportDigest             string                       `json:"driftReportDigest,omitempty"`
	CleanupEvidenceRef            string                       `json:"cleanupEvidenceRef,omitempty"`
	CleanupEvidenceDigest         string                       `json:"cleanupEvidenceDigest,omitempty"`
}

// LastConvergedSourceClosure is the immutable evidence required to safely
// reconstruct a later Destroy. It is written for both Apply and NoChange.
type LastConvergedSourceClosure struct {
	SourceClosureRef                      string `json:"sourceClosureRef"`
	SourceClosureDigest                   string `json:"sourceClosureDigest"`
	BackendSnapshotRef                    string `json:"backendSnapshotRef"`
	BackendSnapshotDigest                 string `json:"backendSnapshotDigest"`
	TargetDiscoveryRef                    string `json:"targetDiscoveryRef"`
	TargetDiscoveryDigest                 string `json:"targetDiscoveryDigest"`
	EffectivePlanInputDigest              string `json:"effectivePlanInputDigest"`
	InfrastructureExecutionIdentityDigest string `json:"infrastructureExecutionIdentityDigest"`
	ExecutionPlatformIdentityDigest       string `json:"executionPlatformIdentityDigest"`
	RunnerServiceAccountIdentityDigest    string `json:"runnerServiceAccountIdentityDigest"`
	TerraformRunUID                       string `json:"terraformRunUID"`
	Generation                            int64  `json:"generation"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=trun
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Operation",type="string",JSONPath=".spec.operation"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type TerraformRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TerraformRunSpec   `json:"spec,omitempty"`
	Status TerraformRunStatus `json:"status,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="TerraformRun spec is immutable"
type TerraformRunSpec struct {
	StackRef                              corev1.LocalObjectReference   `json:"stackRef"`
	InfraStackGeneration                  int64                         `json:"infraStackGeneration"`
	Operation                             string                        `json:"operation"`
	PlanMode                              string                        `json:"planMode"`
	Source                                PlanRunSourceSpec             `json:"source"`
	ResolvedBackendConfigRef              string                        `json:"resolvedBackendConfigRef,omitempty"`
	BackendConfigArtifactRef              string                        `json:"backendConfigArtifactRef,omitempty"`
	BackendConfigArtifactDigest           string                        `json:"backendConfigArtifactDigest,omitempty"`
	VariableSecretRefs                    []corev1.LocalObjectReference `json:"variableSecretRefs,omitempty"`
	VariableSecretVariables               []SecretVariableReference     `json:"variableSecretVariables,omitempty"`
	LockTimeout                           metav1.Duration               `json:"lockTimeout,omitempty"`
	Workspace                             string                        `json:"workspace"`
	Executor                              ExecutorSpec                  `json:"executor"`
	ExecutionContextDigest                string                        `json:"executionContextDigest,omitempty"`
	ExecutionTargetIdentityDigest         string                        `json:"executionTargetIdentityDigest,omitempty"`
	ExecutionPlatformIdentityDigest       string                        `json:"executionPlatformIdentityDigest,omitempty"`
	PlanRunUID                            string                        `json:"planRunUID,omitempty"`
	PlanRef                               string                        `json:"planRef,omitempty"`
	PlanDigest                            string                        `json:"planDigest,omitempty"`
	PlanReportRef                         string                        `json:"planReportRef,omitempty"`
	PlanReportDigest                      string                        `json:"planReportDigest,omitempty"`
	ApprovalRef                           *corev1.LocalObjectReference  `json:"approvalRef,omitempty"`
	ApprovalUID                           string                        `json:"approvalUID,omitempty"`
	EffectivePlanInputDigest              string                        `json:"effectivePlanInputDigest,omitempty"`
	SourceClosureDigest                   string                        `json:"sourceClosureDigest,omitempty"`
	VariablesSnapshotDigest               string                        `json:"variablesSnapshotDigest,omitempty"`
	SecretVariableIdentityDigest          string                        `json:"secretVariableIdentityDigest,omitempty"`
	InfrastructureExecutionIdentityDigest string                        `json:"infrastructureExecutionIdentityDigest,omitempty"`
	RuntimeTargetIdentityDigest           string                        `json:"runtimeTargetIdentityDigest,omitempty"`
	TargetConnectionProfileDigest         string                        `json:"targetConnectionProfileDigest,omitempty"`
	RunnerServiceAccountIdentityDigest    string                        `json:"runnerServiceAccountIdentityDigest,omitempty"`
	TerraformLockfileDigest               string                        `json:"terraformLockfileDigest,omitempty"`
	ExpectedTerraformVersion              string                        `json:"expectedTerraformVersion,omitempty"`
	RunnerImageDigest                     string                        `json:"runnerImageDigest,omitempty"`
	MutationFence                         bool                          `json:"mutationFence,omitempty"`
	RunnerServiceAccountName              string                        `json:"runnerServiceAccountName,omitempty"`
	ConcurrencyGroup                      string                        `json:"concurrencyGroup,omitempty"`
	MaxConcurrentPlans                    int32                         `json:"maxConcurrentPlans,omitempty"`
	MaxConcurrentApplies                  int32                         `json:"maxConcurrentApplies,omitempty"`
}

type PlanRunSourceSpec struct {
	// +kubebuilder:validation:Enum=GitCommit;RetainedBundle
	Type     string `json:"type"`
	URL      string `json:"url,omitempty"`
	Revision string `json:"revision,omitempty"`
	Path     string `json:"path,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Digest   string `json:"digest,omitempty"`
}

type TerraformRunStatus struct {
	ObservedGeneration          int64                        `json:"observedGeneration,omitempty"`
	Conditions                  []metav1.Condition           `json:"conditions,omitempty"`
	JobRef                      *corev1.LocalObjectReference `json:"jobRef,omitempty"`
	SourceBundleRef             string                       `json:"sourceBundleRef,omitempty"`
	SourceBundleDigest          string                       `json:"sourceBundleDigest,omitempty"`
	PlanReportRef               string                       `json:"planReportRef,omitempty"`
	PlanReportDigest            string                       `json:"planReportDigest,omitempty"`
	TargetDiscoveryRef          string                       `json:"targetDiscoveryRef,omitempty"`
	TargetDiscoveryDigest       string                       `json:"targetDiscoveryDigest,omitempty"`
	TerminalResultRef           string                       `json:"terminalResultRef,omitempty"`
	TerminalResultDigest        string                       `json:"terminalResultDigest,omitempty"`
	ResolvedBackendConfigRef    string                       `json:"resolvedBackendConfigRef,omitempty"`
	ResolvedBackendConfigDigest string                       `json:"resolvedBackendConfigDigest,omitempty"`
	TerraformLockfileDigest     string                       `json:"terraformLockfileDigest,omitempty"`
	PlanRef                     string                       `json:"planRef,omitempty"`
	PlanDigest                  string                       `json:"planDigest,omitempty"`
	EffectivePlanInputDigest    string                       `json:"effectivePlanInputDigest,omitempty"`
	HasChanges                  bool                         `json:"hasChanges,omitempty"`
	PlanCreatedAt               *metav1.Time                 `json:"planCreatedAt,omitempty"`
	PlanExpiresAt               *metav1.Time                 `json:"planExpiresAt,omitempty"`
	ExecutionOutcome            string                       `json:"executionOutcome,omitempty"`
	MutationClassification      string                       `json:"mutationClassification,omitempty"`
	MutationMayHaveOccurred     bool                         `json:"mutationMayHaveOccurred,omitempty"`
	ArtifactsReady              bool                         `json:"artifactsReady,omitempty"`
	EvidenceCaptured            bool                         `json:"evidenceCaptured,omitempty"`
	JobUID                      string                       `json:"jobUID,omitempty"`
	TerminalStartedAt           *metav1.Time                 `json:"terminalStartedAt,omitempty"`
	TerminalFinishedAt          *metav1.Time                 `json:"terminalFinishedAt,omitempty"`
	ObservedStateLineage        string                       `json:"observedStateLineage,omitempty"`
	ObservedStateSerial         int64                        `json:"observedStateSerial,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=rset
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ResourceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceSetSpec   `json:"spec,omitempty"`
	Status ResourceSetStatus `json:"status,omitempty"`
}

type ResourceSetSpec struct {
	Target                      TargetReference `json:"target"`
	OwnershipID                 string          `json:"ownershipID,omitempty"`
	MaxInventoryItems           int32           `json:"maxInventoryItems,omitempty"`
	Resources                   []RuntimeObject `json:"resources,omitempty"`
	RuntimeMutationAllowed      bool            `json:"runtimeMutationAllowed,omitempty"`
	MutationFence               bool            `json:"mutationFence,omitempty"`
	RuntimeTargetIdentityDigest string          `json:"runtimeTargetIdentityDigest,omitempty"`
}

type RuntimeObject struct {
	Group           string               `json:"group,omitempty"`
	Version         string               `json:"version"`
	Resource        string               `json:"resource"`
	Wave            int32                `json:"wave,omitempty"`
	ReadinessPolicy string               `json:"readinessPolicy,omitempty"`
	DeletionPolicy  string               `json:"deletionPolicy,omitempty"`
	OwnershipID     string               `json:"ownershipID,omitempty"`
	Object          apiextensionsv1.JSON `json:"object"`
}

type InventoryItemStatus struct {
	TargetIdentityDigest string `json:"targetIdentityDigest"`
	Group                string `json:"group"`
	Version              string `json:"version"`
	Resource             string `json:"resource"`
	Kind                 string `json:"kind"`
	Namespace            string `json:"namespace,omitempty"`
	Name                 string `json:"name"`
	UID                  string `json:"uid"`
	OwnershipID          string `json:"ownershipID,omitempty"`
	ReadinessPolicy      string `json:"readinessPolicy,omitempty"`
	DeletionPolicy       string `json:"deletionPolicy,omitempty"`
}

type ResourceSetStatus struct {
	ObservedGeneration     int64                 `json:"observedGeneration,omitempty"`
	Conditions             []metav1.Condition    `json:"conditions,omitempty"`
	TargetIdentityDigest   string                `json:"targetIdentityDigest,omitempty"`
	Inventory              []InventoryItemStatus `json:"inventory,omitempty"`
	InventoryItems         int32                 `json:"inventoryItems,omitempty"`
	InventoryLimitExceeded bool                  `json:"inventoryLimitExceeded,omitempty"`
	RuntimeCleanupSkipped  bool                  `json:"runtimeCleanupSkipped,omitempty"`
	MutationBlocked        bool                  `json:"mutationBlocked,omitempty"`
	CleanupEvidenceRef     string                `json:"cleanupEvidenceRef,omitempty"`
	CleanupEvidenceDigest  string                `json:"cleanupEvidenceDigest,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=approval
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Plan",type="string",JSONPath=".spec.planRunRef.name"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ChangeApproval struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ChangeApprovalSpec   `json:"spec,omitempty"`
	Status ChangeApprovalStatus `json:"status,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ChangeApproval spec is immutable"
type ChangeApprovalSpec struct {
	PlanRunRef               corev1.LocalObjectReference `json:"planRunRef"`
	PlanRunUID               string                      `json:"planRunUID"`
	PlanDigest               string                      `json:"planDigest"`
	ExecutionContextDigest   string                      `json:"executionContextDigest"`
	EffectivePlanInputDigest string                      `json:"effectivePlanInputDigest"`
	PlanReportRef            string                      `json:"planReportRef"`
	PlanReportDigest         string                      `json:"planReportDigest"`
}

type ChangeApprovalStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ApprovedAt         *metav1.Time       `json:"approvedAt,omitempty"`
}

// +kubebuilder:object:root=true
type PlatformEnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformEnvironment `json:"items"`
}

// +kubebuilder:object:root=true
type InfraStackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InfraStack `json:"items"`
}

// +kubebuilder:object:root=true
type TerraformRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TerraformRun `json:"items"`
}

// +kubebuilder:object:root=true
type ResourceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceSet `json:"items"`
}

// +kubebuilder:object:root=true
type ChangeApprovalList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ChangeApproval `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=vkc
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ValkeyCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ValkeyClusterSpec   `json:"spec,omitempty"`
	Status ValkeyClusterStatus `json:"status,omitempty"`
}

type ValkeyClusterSpec struct {
	Replicas int32  `json:"replicas"`
	Image    string `json:"image"`
}

type ValkeyClusterStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	ReadyReplicas      int32              `json:"readyReplicas,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type ValkeyClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ValkeyCluster `json:"items"`
}

// +kubebuilder:object:root=true
type EnvironmentClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EnvironmentClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&PlatformEnvironment{}, &PlatformEnvironmentList{},
		&InfraStack{}, &InfraStackList{},
		&TerraformRun{}, &TerraformRunList{},
		&ResourceSet{}, &ResourceSetList{},
		&ChangeApproval{}, &ChangeApprovalList{},
		&ValkeyCluster{}, &ValkeyClusterList{},
		&EnvironmentClass{}, &EnvironmentClassList{},
	)
}
