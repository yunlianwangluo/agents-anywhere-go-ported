import type { Context } from '@deepseek-ai/cordis'
import type { ApprovalOutcome, ApprovalRequestEvent } from '@deepseek-ai/dsh-user-approval/types'
import type { SessionEvent, SessionId } from '@deepseek-ai/dsh-session'
import { itemId, sessionId } from './identity.js'

interface Approval {
  id: string
  agentId: string
  toolName: string
  callId?: string
  reason?: string
  status: 'open' | 'responding' | 'resolved' | 'closed' | 'cancelled'
  outcome?: ApprovalOutcome
  revision: number
  resolve?: (outcome: ApprovalOutcome) => void
}

/** Bridges visible native approval waits to platform interaction notices. */
export class UserApprovals {
  private entries = new Map<string, Approval>()
  private closed = false

  constructor(private ctx: Context, private visible: (id: string) => Promise<boolean>,
    private changed: (id?: string) => void) {
    ctx.on('approval/request', async (request, next) => this.receive(request, next), { global: true })
  }

  get available(): boolean { return Boolean(this.ctx.get('approval')) && !this.closed }

  private async receive(request: ApprovalRequestEvent, next: () => Promise<ApprovalOutcome>): Promise<ApprovalOutcome> {
    const id = request.agent.id
    if (this.closed || !await this.visible(id)) return next()
    const events = request.agent.session.snapshotEvents()
    const decided = new Set(events.filter(event => event.type === 'approval/decided').map(event => event.data.id))
    const asked = events.findLast(event => event.type === 'approval/asked' && !decided.has(event.data.id))
    if (!asked || asked.type !== 'approval/asked') return next()
    const entry: Approval = this.entries.get(asked.data.id) ?? {
      id: asked.data.id,
      agentId: id,
      toolName: asked.data.toolName,
      ...(asked.data.callId ? { callId: asked.data.callId } : {}),
      ...(asked.data.reason ? { reason: asked.data.reason } : {}),
      status: 'open',
      revision: 1,
    }
    this.entries.set(entry.id, entry)
    return new Promise<ApprovalOutcome>(resolve => {
      entry.resolve = resolve
      request.signal?.addEventListener('abort', () => this.finish(entry, 'cancelled'), { once: true })
      this.changed(id)
    })
  }

  private pending(entry: Approval): boolean { return entry.status === 'open' || entry.status === 'responding' }
  waiting(id: string): boolean { return [...this.entries.values()].some(entry => entry.agentId === id && this.pending(entry) && entry.resolve) }

  private update(entry: Approval, status: Approval['status'], outcome?: ApprovalOutcome): void {
    if (entry.status === status && entry.outcome === outcome) return
    entry.status = status
    if (outcome === undefined) delete entry.outcome
    else entry.outcome = outcome
    entry.revision++
    this.changed(entry.agentId)
    const closed = [...this.entries.values()].filter(value => !this.pending(value))
    for (const old of closed.slice(0, Math.max(0, closed.length - 128))) this.entries.delete(old.id)
  }
  private finish(entry: Approval, outcome: ApprovalOutcome): void {
    if (!entry.resolve || entry.status !== 'open') return
    const resolve = entry.resolve
    delete entry.resolve
    this.update(entry, 'responding')
    resolve(outcome)
  }

  notices(namespace: string, id: string) {
    const platformId = sessionId(namespace, id)
    return [...this.entries.values()].filter(entry => entry.agentId === id).map(entry => {
      const pending = entry.status === 'open' && Boolean(entry.resolve)
      const context = { toolName: entry.toolName, ...(entry.callId ? { callId: entry.callId } : {}), ...(entry.reason ? { reason: entry.reason } : {}) }
      return { noticeId: itemId(platformId, 'approval', entry.id), sessionId: platformId, runtime: 'dsh',
        type: 'interaction', interactionType: 'approval', title: `需要授权工具：${entry.toolName}`, severity: 'warning',
        status: entry.status, revision: entry.revision, responseRequired: pending,
        blocking: pending ? { scope: 'session', targetId: platformId } : null,
        source: { runtime: 'dsh', component: 'dsh.user_approval' }, context, metadata: { approvalId: entry.id, ...context, ...(entry.outcome ? { outcome: entry.outcome } : {}) },
        actions: pending ? [
          { actionId: 'allow_once', label: '允许一次', style: 'primary', input: { required: false } },
          { actionId: 'reject', label: '拒绝', style: 'secondary', input: { required: false } },
        ] : [] }
    })
  }

  async respond(namespace: string, id: string, noticeId: string, actionId: string) {
    const entry = [...this.entries.values()].find(value => value.agentId === id && itemId(sessionId(namespace, id), 'approval', value.id) === noticeId)
    if (!entry || entry.status !== 'open' || !entry.resolve || !await this.visible(id)) return { ok: false, code: 'dsh_approval_not_pending', message: '这个授权已处理或已失效。' }
    const outcome = actionId === 'allow_once' ? 'allowed-once' : actionId === 'reject' ? 'rejected' : undefined
    if (!outcome) return { ok: false, code: 'dsh_approval_invalid_action', message: '未知的授权操作。' }
    if (entry.status !== 'open' || !entry.resolve) return { ok: false, code: 'dsh_approval_not_pending', message: '这个授权正在处理。' }
    this.finish(entry, outcome)
    return { ok: true, result: { resolved: true, noticeId, sessionId: sessionId(namespace, id), outcome } }
  }

  observe(id: SessionId, event: SessionEvent): void {
    if (event.type === 'approval/asked') {
      const entry = this.entries.get(event.data.id)
      if (entry) {
        entry.toolName = event.data.toolName
        if (event.data.callId === undefined) delete entry.callId
        else entry.callId = event.data.callId
        if (event.data.reason === undefined) delete entry.reason
        else entry.reason = event.data.reason
      } else {
        this.entries.set(event.data.id, { id: event.data.id, agentId: id, toolName: event.data.toolName,
          ...(event.data.callId ? { callId: event.data.callId } : {}), ...(event.data.reason ? { reason: event.data.reason } : {}),
          status: 'open', revision: 1 })
      }
    } else if (event.type === 'approval/decided') {
      const entry = this.entries.get(event.data.id)
      if (entry) {
        delete entry.resolve
        this.update(entry, event.data.outcome === 'cancelled' ? 'cancelled' : event.data.outcome === 'unavailable' ? 'closed' : 'resolved', event.data.outcome)
      }
    } else if (event.type === 'turn/end') {
      for (const entry of this.entries.values()) if (entry.agentId === id && this.pending(entry)) this.finish(entry, 'cancelled')
    }
  }

  close(): void {
    this.closed = true
    for (const entry of this.entries.values()) this.finish(entry, 'cancelled')
  }
}
