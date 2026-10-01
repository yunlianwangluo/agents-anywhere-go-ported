import assert from 'node:assert/strict'
import test from 'node:test'
import { normalizeTurnError } from '../../src/host/dsh-runtime/sync.js'

test('normalizes an insufficient balance turn error as QUOTA without serializing arbitrary data', () => {
  const error = normalizeTurnError({ kind: 'error', message: 'Insufficient Balance (request_id: ignored)', request_id: 'req-123', providerStatus: 429, secret: { token: 'nope' } })
  assert.deepEqual(error, { code: 'QUOTA', message: 'Insufficient Balance (request_id: ignored)', details: { request_id: 'req-123', providerStatus: 429 } })
})

test('normalizes ordinary turn errors with a stable fallback code', () => {
  assert.deepEqual(normalizeTurnError({ kind: 'error', error: { message: 'provider unavailable', code: 'UPSTREAM', status: 'unavailable' } }),
    { code: 'UPSTREAM', message: 'provider unavailable', details: { status: 'unavailable' } })
  assert.equal(normalizeTurnError({ kind: 'completed' }), undefined)
})
