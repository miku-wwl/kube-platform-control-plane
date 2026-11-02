import assert from 'node:assert/strict'
import test from 'node:test'
import { APIError, request } from '../src/api.ts'

test('API failures retain their stable code and full original diagnostic', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async () => new Response(JSON.stringify({ code: 'DraftGenerationFailed', message: 'connection refused with precise context' }), { status: 502 })
  try {
    await assert.rejects(request('/api/drafts'), error => {
      assert.ok(error instanceof APIError)
      assert.equal(error.code, 'DraftGenerationFailed')
      assert.equal(error.message, 'connection refused with precise context')
      return true
    })
  } finally { globalThis.fetch = original }
})

test('legacy HTTP errors retain their diagnostic without inventing a known code', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async () => new Response(JSON.stringify({ message: 'legacy error' }), { status: 409 })
  try {
    await assert.rejects(request('/api/legacy'), error => error instanceof APIError && error.code === '' && error.message === 'legacy error')
  } finally { globalThis.fetch = original }
})

test('a non-JSON gateway failure has a local API availability code and preserves its status', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async () => new Response('', { status: 502, statusText: 'Bad Gateway' })
  try {
    await assert.rejects(request('/api/drafts'), error => error instanceof APIError && error.code === 'APIUnavailable' && error.message === '502 Bad Gateway')
  } finally { globalThis.fetch = original }
})

test('a disconnected API retains the browser diagnostic behind an availability code', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async () => { throw new TypeError('Failed to fetch') }
  try {
    await assert.rejects(request('/api/drafts'), error => error instanceof APIError && error.code === 'APIUnavailable' && error.message === 'Failed to fetch')
  } finally { globalThis.fetch = original }
})
