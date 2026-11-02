import assert from 'node:assert/strict'
import test from 'node:test'
import { classDiagnosticMessage } from '../src/classValidation.ts'

test('server code selects translation even when original diagnostic changes', () => {
  const diagnostic = { code: 'SourceRevisionUnpinned', field: 'source.revision', message: 'new precise backend diagnostic' }
  const key = 'source.revision must be a pinned 40-64 character hexadecimal commit.'
  assert.equal(classDiagnosticMessage(diagnostic, diagnostic.message, (value) => value === key ? '固定提交哈希' : value), '固定提交哈希')
  assert.equal(diagnostic.message, 'new precise backend diagnostic')
})

test('backend warning and required field use stable code with field context', () => {
  const seen = []
  const t = (key, values) => { seen.push({ key, values }); return `${values.field}: translated` }
  assert.equal(classDiagnosticMessage({code:'Required',field:'version',message:'version is required'}, '', t), 'version: translated')
  classDiagnosticMessage({code:'BackendPrerequisites',field:'backend.configRef',message:'original warning'}, '', t)
  assert.match(seen[1].key, /^Backend configuration must exist/)
  assert.equal(seen[1].values.field, 'backend.configRef')
})

test('unknown codes and legacy errors retain the entire original diagnostic', () => {
  for (const code of ['FutureDiagnostic', 'constructor', '__proto__']) {
    assert.equal(classDiagnosticMessage({code,message:'source.revision must be a pinned commit'}, 'fallback', () => {throw Error('must not translate by text')}), 'source.revision must be a pinned commit')
  }
  assert.equal(classDiagnosticMessage(undefined, 'legacy diagnostic with context', () => {throw Error('must not translate legacy text')}), 'legacy diagnostic with context')
})
