import assert from 'node:assert/strict'
import test from 'node:test'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { createUserMessage } from '@deepseek-ai/dsh-llm'
import { SessionId } from '@deepseek-ai/dsh-session'
import { nativeRuntime } from '../fixtures/native-runtime.js'
import { mountAgents, TextAdapter } from '../fixtures/agent-runtime.js'
import { RuntimeRouter } from '../../src/host/dsh-runtime/router.js'
import { SyncFeed, type SyncBatch, type SyncOperation } from '../../src/host/dsh-runtime/sync.js'
import { sessionId } from '../../src/host/dsh-runtime/identity.js'

async function until(check: () => boolean, label: string) {
  for (let n = 0; n < 500; n++) { if (check()) return; await delay(10) }
  assert.ok(check(), label)
}

function notifications(batches: SyncBatch[]) {
  return batches.flatMap(batch => batch.operations).filter((operation): operation is SyncOperation & { kind: 'notifications', notifications: any[] } => operation.kind === 'notifications').flatMap(operation => operation.notifications)
}

async function fixture() {
  const home = await mkdtemp(join(tmpdir(), 'aa-approvals-'))
  const native = await nativeRuntime(home, ctx => mountAgents(ctx, new TextAdapter()))
  const runtime = native.ctx.agentsAnywhereRuntime.native
  const router = new RuntimeRouter({ native: runtime, query: native.ctx.sessionQuery, status: id => runtime.status(id) }, 'test')
  const request = (method: string, params: Record<string, unknown>) => router.request(method, params, new AbortController().signal)
  return { ...native, home, runtime, request, close: async () => {
    router.close(); await native.ctx.fiber.dispose(); await rm(home, { recursive: true, force: true })
  } }
}

test('SyncFeed publishes pending and resolved native approvals with matching session states', { timeout: 30_000 }, async () => {
  const f = await fixture()
  const id = SessionId('approval-sync')
  const platformId = sessionId('test', id)
  const batches: SyncBatch[] = []
  const feed = new SyncFeed(f.runtime, 'test', batch => { batches.push(batch); queueMicrotask(() => feed.ack(batch.batchSeq)) }, error => assert.fail(String(error)))
  try {
    feed.start()
    await until(() => notifications(batches).some(note => note.method === 'session.inventory.complete'), 'sync baseline')
    const handle = await f.ctx.agents.create({ sessionId: id, agentOptions: { provider: 'test', model: 'text' }, meta: { cwd: f.home } })
    f.ctx.permissionPresets.set(handle.agent.session, 'workspace-write')
    handle.agent.session.append('user/message', createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: '执行工具' }] }), { surfaceOp: 'append' })
    handle.agent.session.append('turn/start', { turn: 1 })
    const approval = f.ctx.approval.request({ agent: handle.agent, toolName: 'fixture-tool', callId: 'call-sync' as never, reason: '需要执行工具', signal: new AbortController().signal })
    await until(() => {
      const notes = notifications(batches)
      return notes.some(note => note.method === 'notice.upsert' && note.params.sessionId === platformId && note.params.status === 'open' && note.params.interactionType === 'approval')
        && notes.some(note => note.method === 'session.state.updated' && note.params.sessionId === platformId && note.params.status === 'waiting_approval')
    }, 'pending approval is published through SyncFeed')
    const pending = notifications(batches).find(note => note.method === 'notice.upsert' && note.params.sessionId === platformId && note.params.status === 'open' && note.params.interactionType === 'approval')!
    assert.equal((await f.request('session.respondInteraction', { sessionId: platformId, noticeId: pending.params.noticeId, actionId: 'allow_once' }) as { ok: boolean }).ok, true)
    assert.equal(await approval, 'allowed-once')
    await until(() => {
      const notes = notifications(batches)
      return notes.some(note => note.method === 'notice.upsert' && note.params.noticeId === pending.params.noticeId && note.params.status === 'resolved')
        && notes.some(note => note.method === 'session.state.updated' && note.params.sessionId === platformId && note.params.status !== 'waiting_approval')
    }, 'resolved approval is published through SyncFeed')
    await handle.dispose()
  } finally { feed.close(); await f.close() }
})

for (const [actionId, outcome] of [['allow_once', 'allowed-once'], ['reject', 'rejected']] as const) {
  test(`visible native approval is answered as ${outcome}`, { timeout: 30_000 }, async () => {
    const f = await fixture()
    const id = SessionId(`approval-${actionId}`)
    const platformId = sessionId('test', id)
    try {
      const handle = await f.ctx.agents.create({ sessionId: id, agentOptions: { provider: 'test', model: 'text' }, meta: { cwd: f.home } })
      f.ctx.permissionPresets.set(handle.agent.session, 'workspace-write')
      handle.agent.session.append('user/message', createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: '执行工具' }] }), { surfaceOp: 'append' })
      handle.agent.session.append('turn/start', { turn: 1 })
      assert.equal(await f.runtime.visible(id), true)
      assert.equal(f.runtime.approvals.available, true)
      assert.equal(handle.agent.id, id)
      const approval = f.ctx.approval.request({ agent: handle.agent, toolName: 'fixture-tool', callId: 'call-1' as never, reason: '需要执行工具', signal: new AbortController().signal })
      await until(() => f.runtime.approvals.waiting(id), 'approval is waiting')
      assert.equal((await f.request('session.getState', { sessionId: platformId }) as { status: string }).status, 'waiting_approval')
      const capabilities = await f.runtime.capabilities(platformId, id)
      assert.equal(capabilities.metadata.approval, true)
      assert.equal(capabilities.capabilities.find((value: { capabilityId: string }) => value.capabilityId === 'session.interaction.approval')?.available, true)
      const notice = (await f.request('session.getNotices', { sessionId: platformId }) as { notices: any[] }).notices.find(value => value.interactionType === 'approval')
      assert.ok(notice)
      assert.equal(notice.severity, 'warning')
      assert.deepEqual(notice.context, { toolName: 'fixture-tool', callId: 'call-1', reason: '需要执行工具' })
      assert.deepEqual(notice.actions.map((value: any) => [value.actionId, value.input.required]), [['allow_once', false], ['reject', false]])
      const responses = await Promise.all([
        f.request('session.respondInteraction', { sessionId: platformId, noticeId: notice.noticeId, actionId }),
        f.request('session.respondInteraction', { sessionId: platformId, noticeId: notice.noticeId, actionId }),
      ]) as { ok: boolean }[]
      assert.equal(responses.filter(value => value.ok).length, 1)
      assert.equal(await approval, outcome)
      const audit = handle.agent.session.snapshotEvents().filter(event => event.type === 'approval/asked' || event.type === 'approval/decided')
      assert.equal(audit[0]?.data.id, notice.metadata.approvalId)
      assert.equal(audit[1]?.data.id, notice.metadata.approvalId)
      await until(() => !f.runtime.approvals.waiting(id), 'approval is settled')
      const closed = (await f.request('session.getNotices', { sessionId: platformId }) as { notices: any[] }).notices.find(value => value.noticeId === notice.noticeId)
      assert.equal(closed.status, 'resolved')
      assert.equal(closed.responseRequired, false)
      assert.notEqual((await f.request('session.getState', { sessionId: platformId }) as { status: string }).status, 'waiting_approval')
      await handle.dispose()
    } finally { await f.close() }
  })
}
