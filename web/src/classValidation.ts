import type { ClassValidationDiagnostic } from './api'

// Only explicit server codes select translations. Unknown/older diagnostics
// remain visible verbatim; no matching of backend message fragments is used.
const messages: Record<string, string> = {
  Required: '{{field}} is required.',
  KubernetesNameInvalid: '{{field}} must be a valid Kubernetes name.',
  SourceRevisionUnpinned: 'source.revision must be a pinned 40-64 character hexadecimal commit.',
  SourcePathInvalid: 'Source path must stay within the repository.',
  BackendTypeUnsupported: 'Only the S3 backend is supported.',
  ServiceAccountMismatch: 'Backend and runner service accounts must match.',
  PositiveRequired: '{{field}} must be positive.',
  RunnerMismatch: 'Executor and runner image / Terraform version must match.',
  TargetInvalid: 'The target identity or connection profile is invalid.',
  AllowedRegionsInvalid: 'Allowed regions must be unique and non-empty.',
  RegionNotAllowed: 'Default region must be included in allowed regions.',
  CapacityNegative: 'Capacity limits cannot be negative.',
  CapacityInverted: 'Minimum node count cannot exceed maximum.',
  ApprovalPolicyInvalid: 'Approval policy must be Manual or Automatic.',
  RuntimeObjectIncomplete: '{{field}} requires version, resource and object.',
  RuntimeObjectInvalid: '{{field}} must be a JSON object.',
  InlineCredentialsForbidden: 'Use credential references; inline credentials and Secret resources are not accepted.',
  ExecutionProfileInvalid: 'The emulator execution profile is invalid.',
  ResolvedCredentialsForbidden: 'Do not supply resolved credential identity in the class.',
  AdmissionRejected: 'Kubernetes admission rejected this class configuration.',
  BackendPrerequisites: 'Backend configuration must exist in each environment namespace. Ready confirms class admission and controller reconciliation; source access, runner images and infrastructure execution are checked later.',
}

export function classDiagnosticMessage(
  diagnostic: ClassValidationDiagnostic | undefined,
  fallback: string,
  t: (key: string, values?: Record<string, string | number>) => string,
): string {
  const key = diagnostic && Object.hasOwn(messages, diagnostic.code) ? messages[diagnostic.code] : undefined
  return key ? t(key, { field: diagnostic?.field ?? '' }) : diagnostic?.message ?? fallback
}
