import type { ClassSpec, ClassTemplate } from './api'

export type TemplateChoice = { id: string; title: string; description: string; suggestedName: string; spec: ClassSpec; configured: boolean }

export function defaultClassSpec(): ClassSpec {
  return {
    version: 'v1', source: { url: '', revision: '', path: '.' },
    backend: { type: 's3', configRef: { name: '' }, authRef: { serviceAccountName: 'terraform-runner' }, lockTimeout: '2m' },
    executor: { terraformVersion: '1.14.0', image: '', workDir: '/workspace/terraform', executionTimeout: '12m' },
    runnerProfile: { serviceAccountName: 'terraform-runner', image: '', imageDigest: '', terraformVersion: '1.14.0' },
    runtimeProfile: {}, target: { provider: 'kind', account: 'local', region: 'local', clusterName: '' },
    allowedRegions: ['local'], capacityBounds: { minNodeCount: 1, maxNodeCount: 10, maxEnvironments: 10, maxConcurrentPlans: 2, maxConcurrentApplies: 1 },
    approvalPolicy: 'Manual',
  }
}

// Starter presets supply policy defaults. Project-specific references are filled
// once, then saved as a reusable template instead of guessing available assets.
export function starterTemplates(): TemplateChoice[] {
  const small = defaultClassSpec()
  small.capacityBounds = { minNodeCount: 1, maxNodeCount: 3, maxEnvironments: 3, maxConcurrentPlans: 1, maxConcurrentApplies: 1 }
  return [
    { id: 'kind-small', title: 'Kind · Small development', description: '1–3 nodes, up to 3 environments, manual approval.', suggestedName: 'local-small-v1', spec: small, configured: false },
    { id: 'kind-team', title: 'Kind · Team development', description: '1–10 nodes, up to 10 environments, two concurrent plans, manual approval.', suggestedName: 'local-team-v1', spec: defaultClassSpec(), configured: false },
  ]
}

export function savedTemplateChoice(template: ClassTemplate): TemplateChoice {
  return { id: template.uid, title: template.title, description: template.description, suggestedName: `${template.name}-${template.spec.version}`, spec: structuredClone(template.spec), configured: true }
}

export function availableClassName(suggested: string, existing: string[]): string {
  const base = suggested.toLowerCase().replace(/[^a-z0-9.-]/g, '-').replace(/^[.-]+|[.-]+$/g, '').slice(0, 220).replace(/[.-]+$/g, '') || 'new-class'
  const used = new Set(existing)
  if (!used.has(base)) return base
  let suffix = 2
  while (used.has(`${base}-${suffix}`)) suffix++
  return `${base}-${suffix}`
}
