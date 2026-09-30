package httpapi

import (
	beego "github.com/beego/beego/v2/server/web"
	beegoctx "github.com/beego/beego/v2/server/web/context"
)

// Register binds every client-facing route onto beego.
func Register(c *Controller) {
	beego.InsertFilter("/*", beego.BeforeRouter, c.requestLog)
	beego.ErrorController(&apiController{})

	beego.Get("/", c.loginPage)
	beego.Get("/#/mobile-oauth", c.loginPage)

	// WebSocket surfaces.
	beego.Any("/api/v2/connector/ws", c.auth(func(ctx *beegoctx.Context) { c.connectorWS(ctx) }))
	beego.Any("/api/v2/events", c.auth(func(ctx *beegoctx.Context) { c.clientEvents(ctx) }))
	beego.Any("/api/v2/dashboard/ws", c.auth(func(ctx *beegoctx.Context) { c.dashboardWS(ctx) }))
	beego.Any("/api/v2/connector/terminals/:terminalId/relay", c.auth(func(ctx *beegoctx.Context) { c.terminalRelay(ctx) }))

	// Authentication and health.
	beego.Get("/api/v2/health", c.health)
	beego.Get("/api/v2/auth/config", c.authConfig)
	beego.Get("/api/v2/auth/me", c.auth(c.authMe))
	beego.Get("/api/v2/oauth/authorize", c.oauthAuthorize)
	beego.Post("/api/v2/oauth/authorize", c.oauthAuthorizeSubmit)
	beego.Post("/api/v2/oauth/token", c.oauthToken)

	// Connectors and runtimes.
	beego.Get("/api/v2/connectors", c.auth(c.deviceList))
	beego.Get("/api/v2/connectors/:id", c.auth(c.deviceGet))
	beego.Get("/api/v2/connectors/:id/runtimes/:runtimeId", c.auth(c.runtimeGet))
	beego.Get("/api/v2/connectors/:id/runtimes/:runtimeId/capabilities", c.auth(c.runtimeCapabilities))
	beego.Get("/api/v2/connectors/:id/runtimes/:runtimeId/catalogs/model", c.auth(c.modelCatalog))
	beego.Get("/api/v2/connectors/:id/runtimes/:runtimeId/catalogs/permission", c.auth(c.permissionCatalog))
	beego.Get("/api/v2/connectors/:id/runtimes/:runtimeId/commands", c.auth(c.runtimeCommands))
	beego.Get("/api/v2/connectors/:id/preferences", c.auth(c.connectorPreferences))
	beego.Get("/api/v2/connectors/:id/runtimes", c.auth(c.runtimeList))
	beego.Post("/api/v2/connectors/:id/runtimes/discover", c.auth(c.runtimeList))
	beego.Post("/api/v2/connectors/:id/fs/list", c.auth(c.workspaceList))

	// Projects.
	beego.Get("/api/v2/projects", c.auth(c.projectList))
	beego.Patch("/api/v2/projects/:id", c.auth(c.projectPatch))
	beego.Delete("/api/v2/projects/:id", c.auth(c.projectDelete))
	beego.Get("/api/v2/projects/:id/sessions", c.auth(c.projectSessions))
	beego.Post("/api/v2/projects/:id/sessions/archive-all", c.auth(func(ctx *beegoctx.Context) { c.sessionBulk(ctx, "archive") }))
	beego.Post("/api/v2/projects", c.auth(c.createProject))

	// Sessions.
	beego.Get("/api/v2/sessions", c.auth(c.sessionList))
	beego.Get("/api/v2/sessions/list", c.auth(c.sessionList))
	beego.Post("/api/v2/sessions/create-and-start", c.auth(c.createAndStart))
	beego.Post("/api/v2/ws-ticket", c.auth(c.wsTicket))
	beego.Get("/api/v2/sessions/:id/meta", c.auth(c.sessionMeta))
	beego.Patch("/api/v2/sessions/:id/meta", c.auth(c.sessionMetaPatch))
	beego.Post("/api/v2/sessions/read", c.auth(func(ctx *beegoctx.Context) { c.sessionBulk(ctx, "read") }))
	beego.Post("/api/v2/sessions/archive", c.auth(func(ctx *beegoctx.Context) { c.sessionBulk(ctx, "archive") }))
	beego.Post("/api/v2/sessions/unarchive", c.auth(func(ctx *beegoctx.Context) { c.sessionBulk(ctx, "unarchive") }))
	beego.Get("/api/v2/sessions/:id/takeover", c.auth(c.sessionMeta))
	beego.Post("/api/v2/sessions/:id/takeover", c.auth(c.sessionTakeover))
	beego.Delete("/api/v2/sessions/:id/takeover", c.auth(c.sessionTakeover))
	beego.Post("/api/v2/sessions/:id/sync", c.auth(c.sessionSync))

	// Live session surfaces.
	beego.Any("/api/v2/sessions/:id/ws", c.auth(c.sessionWS))
	beego.Get("/api/v2/sessions/:id/events", c.auth(c.sessionEvents))
	beego.Get("/api/v2/sessions/:id/runtime/capabilities", c.auth(c.sessionRuntimeCapabilities))
	beego.Get("/api/v2/sessions/:id/runtime/state", c.auth(c.sessionRuntimeState))
	beego.Get("/api/v2/sessions/:id/runtime/notices", c.auth(c.sessionRuntimeNotices))
	beego.Post("/api/v2/sessions/:id/runtime/notices/:noticeId/respond", c.auth(c.respondInteraction))
	beego.Get("/api/v2/sessions/:id/runtime/catalogs/model", c.auth(c.sessionModelCatalog))
	beego.Get("/api/v2/sessions/:id/runtime/catalogs/permission", c.auth(c.sessionPermissionCatalog))
	beego.Patch("/api/v2/sessions/:id/runtime/selections", c.auth(func(ctx *beegoctx.Context) { c.sessionRuntimeAction(ctx, "runtime.setSelections") }))
	beego.Get("/api/v2/sessions/:id/runtime/commands", c.auth(func(ctx *beegoctx.Context) { c.sessionRuntimeAction(ctx, "runtime.listCommands") }))
	beego.Post("/api/v2/sessions/:id/runtime/commands", c.auth(func(ctx *beegoctx.Context) { c.sessionRuntimeAction(ctx, "runtime.executeCommand") }))
	beego.Post("/api/v2/sessions/:id/runtime/steer", c.auth(func(ctx *beegoctx.Context) { c.sessionRuntimeAction(ctx, "session.startTurn") }))
	beego.Get("/api/v2/sessions/:id/timeline", c.auth(c.sessionTimeline))
	beego.Get("/api/v2/sessions/:id/snapshot", c.auth(c.sessionSnapshot))
	beego.Get("/api/v2/sessions/:id", c.auth(c.sessionDetail))

	// Attachments.
	beego.Post("/api/v2/attachments", c.auth(c.uploadAttachment))
	beego.Post("/api/v2/sessions/:id/attachments", c.auth(c.uploadAttachment))
	beego.Get("/api/v2/attachments/:fileId", c.auth(c.downloadAttachment))
	beego.Get("/api/v2/sessions/:id/attachments/:fileId", c.auth(c.downloadAttachment))
	// The connector fetches raw bytes here before staging them for the bridge.
	beego.Get("/api/v2/connector/sessions/:id/attachments/:fileId/content", c.auth(c.connectorAttachmentContent))

	// Runtime actions forwarded to the connector.
	beego.Post("/api/v2/sessions", c.auth(func(ctx *beegoctx.Context) { c.forwardSession(ctx, "session.createAndStart") }))
	beego.Post("/api/v2/sessions/:id/messages", c.auth(func(ctx *beegoctx.Context) { c.forwardSession(ctx, "session.startTurn") }))
	beego.Post("/api/v2/sessions/:id/runtime/messages", c.auth(func(ctx *beegoctx.Context) { c.forwardSession(ctx, "session.startTurn") }))
	beego.Post("/api/v2/sessions/:id/runtime/interrupt", c.auth(func(ctx *beegoctx.Context) { c.forwardSession(ctx, "session.interrupt") }))
	beego.Post("/api/v2/sessions/:id/interrupt", c.auth(func(ctx *beegoctx.Context) { c.forwardSession(ctx, "session.interrupt") }))
	beego.Post("/api/v2/sessions/:id/interaction/respond", c.auth(func(ctx *beegoctx.Context) { c.forwardSession(ctx, "session.respondInteraction") }))
}
