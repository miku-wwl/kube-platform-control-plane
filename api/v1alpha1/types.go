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
	InfraStackRef corev1.LocalObjectReference `json:"infraStackRef"`
	Target        TargetReference             `json:"target"`
	DesiredState  DesiredState                `json:"desiredState,omitempty"`
}

type PlatformEnvironmentStatus struct {
	ObservedGeneration int64                        `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition           `json:"conditions,omitempty"`
	InfraStackRef      *corev1.LocalObjectReference `json:"infraStackRef,omitempty"`
	ResourceSetRef     *corev1.LocalObjectReference `json:"resourceSetRef,omitempty"`
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
	Variables      VariablesSpec  `json:"variables,omitempty"`
	Executor       ExecutorSpec   `json:"executor"`
	DesiredState   DesiredState   `json:"desiredState,omitempty"`
	ApprovalPolicy ApprovalPolicy `json:"approvalPolicy,omitempty"`
}

type SourceSpec struct {
	URL string `json:"url"`
	// +kubebuilder:validation:Pattern=`^[0-9a-fA-F]{40,64}$`
	Revision string `json:"revision"`
	Path     string `json:"path"`
}

type BackendSpec struct {
	// +kubebuilder:validation:Enum=s3
	Type        string                      `json:"type"`
	ConfigRef   corev1.LocalObjectReference `json:"configRef"`
	AuthRef     ServiceAccountReference     `json:"authRef"`
	LockTimeout metav1.Duration             `json:"lockTimeout,omitempty"`
}

type ServiceAccountReference struct {
	ServiceAccountName string `json:"serviceAccountName"`
}

type VariablesSpec struct {
	SecretRefs []corev1.LocalObjectReference `json:"secretRefs,omitempty"`
}

type ExecutorSpec struct {
	TerraformVersion    string          `json:"terraformVersion"`
	Image               string          `json:"image"`
	WorkDir             string          `json:"workDir"`
	ExecutionTimeout    metav1.Duration `json:"executionTimeout"`
	Parallelism         *int32          `json:"parallelism,omitempty"`
	RuntimeOS           string          `json:"runtimeOS,omitempty"`
	RuntimeArchitecture string          `json:"runtimeArchitecture,omitempty"`
}

type TargetReference struct {
	Provider    string `json:"provider"`
	Account     string `json:"account,omitempty"`
	Region      string `json:"region,omitempty"`
	ClusterName string `json:"clusterName"`
	ClusterID   string `json:"clusterID,omitempty"`
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

type TerraformRunSpec struct {
	StackRef                        corev1.LocalObjectReference   `json:"stackRef"`
	InfraStackGeneration            int64                         `json:"infraStackGeneration"`
	Operation                       string                        `json:"operation"`
	PlanMode                        string                        `json:"planMode"`
	Source                          PlanRunSourceSpec             `json:"source"`
	ResolvedBackendConfigRef        string                        `json:"resolvedBackendConfigRef,omitempty"`
	BackendConfigArtifactRef        string                        `json:"backendConfigArtifactRef,omitempty"`
	BackendConfigArtifactDigest     string                        `json:"backendConfigArtifactDigest,omitempty"`
	VariableSecretRefs              []corev1.LocalObjectReference `json:"variableSecretRefs,omitempty"`
	LockTimeout                     metav1.Duration               `json:"lockTimeout,omitempty"`
	Workspace                       string                        `json:"workspace"`
	Executor                        ExecutorSpec                  `json:"executor"`
	ExecutionContextDigest          string                        `json:"executionContextDigest,omitempty"`
	ExecutionTargetIdentityDigest   string                        `json:"executionTargetIdentityDigest,omitempty"`
	ExecutionPlatformIdentityDigest string                        `json:"executionPlatformIdentityDigest,omitempty"`
	PlanRunUID                      string                        `json:"planRunUID,omitempty"`
	PlanRef                         string                        `json:"planRef,omitempty"`
	PlanDigest                      string                        `json:"planDigest,omitempty"`
	ApprovalRef                     *corev1.LocalObjectReference  `json:"approvalRef,omitempty"`
	ApprovalUID                     string                        `json:"approvalUID,omitempty"`
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
	ResolvedBackendConfigRef    string                       `json:"resolvedBackendConfigRef,omitempty"`
	ResolvedBackendConfigDigest string                       `json:"resolvedBackendConfigDigest,omitempty"`
	TerraformLockfileDigest     string                       `json:"terraformLockfileDigest,omitempty"`
	PlanRef                     string                       `json:"planRef,omitempty"`
	PlanDigest                  string                       `json:"planDigest,omitempty"`
	HasChanges                  bool                         `json:"hasChanges,omitempty"`
	PlanCreatedAt               *metav1.Time                 `json:"planCreatedAt,omitempty"`
	PlanExpiresAt               *metav1.Time                 `json:"planExpiresAt,omitempty"`
	ExecutionOutcome            string                       `json:"executionOutcome,omitempty"`
	ArtifactsReady              bool                         `json:"artifactsReady,omitempty"`
	EvidenceCaptured            bool                         `json:"evidenceCaptured,omitempty"`
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
	Target            TargetReference `json:"target"`
	MaxInventoryItems int32           `json:"maxInventoryItems,omitempty"`
	Resources         []RuntimeObject `json:"resources,omitempty"`
}

type RuntimeObject struct {
	Group    string               `json:"group,omitempty"`
	Version  string               `json:"version"`
	Resource string               `json:"resource"`
	Wave     int32                `json:"wave,omitempty"`
	Object   apiextensionsv1.JSON `json:"object"`
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
}

type ResourceSetStatus struct {
	ObservedGeneration     int64                 `json:"observedGeneration,omitempty"`
	Conditions             []metav1.Condition    `json:"conditions,omitempty"`
	TargetIdentityDigest   string                `json:"targetIdentityDigest,omitempty"`
	Inventory              []InventoryItemStatus `json:"inventory,omitempty"`
	InventoryItems         int32                 `json:"inventoryItems,omitempty"`
	InventoryLimitExceeded bool                  `json:"inventoryLimitExceeded,omitempty"`
	RuntimeCleanupSkipped  bool                  `json:"runtimeCleanupSkipped,omitempty"`
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

type ChangeApprovalSpec struct {
	PlanRunRef             corev1.LocalObjectReference `json:"planRunRef"`
	PlanRunUID             string                      `json:"planRunUID"`
	PlanDigest             string                      `json:"planDigest"`
	ExecutionContextDigest string                      `json:"executionContextDigest"`
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

func init() {
	SchemeBuilder.Register(
		&PlatformEnvironment{}, &PlatformEnvironmentList{},
		&InfraStack{}, &InfraStackList{},
		&TerraformRun{}, &TerraformRunList{},
		&ResourceSet{}, &ResourceSetList{},
		&ChangeApproval{}, &ChangeApprovalList{},
		&ValkeyCluster{}, &ValkeyClusterList{},
	)
}
