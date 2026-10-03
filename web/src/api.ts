export type Condition = { type: string; status: string; reason?: string; message?: string; lastTransitionTime?: string }
export type Target = { provider?: string; accountID?: string; region?: string; clusterName?: string; incarnationID?: string }
export type RunBrief = { name: string; operation: string; outcome?: string; hasChanges?: boolean; reason?: string }
export type Environment = {
  namespace: string; name: string; uid: string; class?: string; generation: number; nodeCount: number; phase: string; ready: boolean
  readyStatus: string; infrastructure: string; runtime: string; target: Target
  latestTerraformRun?: RunBrief; latestApproval?: string; lastTransitionTime?: string; createdAt: string; deleting?: boolean
}
export type ClassInfo = {
  name: string; provider: string; targetCluster: string; defaultRegion?: string; allowedRegions?: string[]
  uid: string; version: string; usageCount: number; createdAt: string; deleting: boolean
  capacityBounds: { minNodeCount?: number; maxNodeCount?: number; maxEnvironments?: number; maxConcurrentPlans?: number; maxConcurrentApplies?: number }; ready: boolean; readyReason?: string
}
export type RuntimeObject = { group?: string; version: string; resource: string; object: Record<string, unknown>; wave?: number; ownershipID?: string; readinessPolicy?: string; deletionPolicy?: string }
export type ClassSpec = {
  version: string
  source: { url: string; revision: string; path: string }
  backend: { type: string; configRef: { name: string }; authRef: { serviceAccountName: string }; lockTimeout: string }
  executor: { terraformVersion: string; image: string; workDir: string; executionTimeout: string; parallelism?: number; runtimeOS?: string; runtimeArchitecture?: string }
  runnerProfile: { serviceAccountName: string; image: string; imageDigest: string; terraformVersion: string; managementRoleARN?: string }
  runtimeProfile: { operatorImage?: string; operatorImageDigest?: string; operatorManifestRef?: string; valkeyImage?: string; networkPolicyProfile?: string; runtimeObjects?: RuntimeObject[] }
  target: { provider: string; account?: string; region?: string; clusterName: string; clusterID?: string; clusterARN?: string; incarnationID?: string; connectionProfileRef?: string; executionRoleARN?: string; runtimeRoleARN?: string }
  allowedRegions?: string[]; capacityBounds: ClassInfo['capacityBounds']; approvalPolicy: 'Manual' | 'Automatic'; dataRetentionPolicy?: string
}
export type ClassDetail = ClassInfo & { spec: ClassSpec; conditions: Condition[]; specDigest?: string; redacted: boolean; yaml: string }
export type ClassValidation = { valid: boolean; errors: string[]; warnings: string[]; yaml?: string }
export type ClassTemplate = { name: string; title: string; description: string; spec: ClassSpec; uid: string; createdAt: string; redacted: boolean }
export type Approval = { namespace: string; name: string; planRun: string; planRunUID: string; state: string; planDigest: string; createdAt: string; approvedAt?: string }
export type TerraformRun = {
  namespace: string; name: string; uid: string; operation: string; planMode?: string; state: string; reason?: string; message?: string
  outcome?: string; hasChanges: boolean; planDigest?: string; planRef?: string; planReportRef?: string; planReportDigest?: string
  effectivePlanInputDigest?: string; executionContextDigest?: string; planExpiresAt?: string; planCreatedAt?: string
  artifactsReady: boolean; evidenceCaptured: boolean; terminalStartedAt?: string; terminalFinishedAt?: string; approval?: Approval; createdAt: string
}
export type TimelineEvent = { at: string; kind: string; resource: string; title: string; state?: string; reason?: string; message?: string }
export type EnvironmentDetail = Environment & {
  conditions: Condition[]; infrastructureDetail?: { name: string; desiredState?: string; state: string; condition?: Condition; latestPlanRun?: string; lastAppliedRun?: string; convergenceEvidence: boolean }
  runtimeDetail?: { name: string; state: string; condition?: Condition; inventoryItems: number; inventoryLimitExceeded: boolean; mutationBlocked: boolean; cleanupEvidence: boolean }
  terraformRuns: TerraformRun[]; approvals: Approval[]; valkey?: { desired: boolean; shards?: number; replicas?: number; readyReplicas?: number; state: string }
  evidence: { planDigest?: string; planReportRef?: string; planReportDigest?: string; terminalResultRef?: string; terminalResultDigest?: string; sourceBundleDigest?: string; targetDiscoveryDigest?: string }
  timeline: TimelineEvent[]; timelineNote: string
}
export type PlanNode = { address: string; type: string; provider?: string; action: 'create' | 'update' | 'delete' | 'replace' | 'no-op'; before?: Record<string, unknown>; after?: Record<string, unknown> }
export type PlanGraph = { version: number; terraformRunUID: string; planDigest: string; nodes: PlanNode[]; edges: { from: string; to: string }[]; summary: { create: number; update: number; delete: number; replace: number; noOp: number } }
export type PlanView = { terraformRun?: TerraformRun; available: boolean; message: string; graph?: PlanGraph }
export type DraftRequest = { description: string; name?: string; namespace: string; classRef: string; region?: string; nodeCount?: number }
export type DraftResponse = { provider: string; intent: { name: string; namespace: string; classRef: string; region?: string; nodeCount: number; valkeyEnabled: boolean; valkeyShards?: number; valkeyReplicas?: number }; valid: boolean; errors?: string[]; warnings?: string[]; yaml?: string }

export async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, { ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(body.message || `${response.status} ${response.statusText}`)
  return body as T
}

export const envPath = (environment: Pick<Environment, 'namespace' | 'name'>) => `/api/environments/${encodeURIComponent(environment.namespace)}/${encodeURIComponent(environment.name)}`
