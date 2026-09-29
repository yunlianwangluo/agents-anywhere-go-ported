package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	beegoctx "github.com/beego/beego/v2/server/web/context"
)

func (c *Controller) deviceList(ctx *beegoctx.Context) {
	status, body := c.server.DeviceList()
	writeJSON(ctx, status, body)
}

func (c *Controller) deviceGet(ctx *beegoctx.Context) {
	status, body := c.server.DeviceGet(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) runtimeGet(ctx *beegoctx.Context) {
	status, body := c.server.RuntimeGet(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) runtimeList(ctx *beegoctx.Context) {
	status, body := c.server.RuntimeList(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) runtimeCapabilities(ctx *beegoctx.Context) {
	status, body := c.server.RuntimeCapabilities(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) modelCatalog(ctx *beegoctx.Context) {
	status, body := c.server.RuntimeCatalog(ctx.Input.Param(":id"), "catalog.listModels", "catalog")
	writeJSON(ctx, status, body)
}

func (c *Controller) permissionCatalog(ctx *beegoctx.Context) {
	status, body := c.server.RuntimeCatalog(ctx.Input.Param(":id"), "catalog.listPermissions", "catalog")
	writeJSON(ctx, status, body)
}

func (c *Controller) runtimeCommands(ctx *beegoctx.Context) {
	status, body := c.server.RuntimeCommands(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) connectorPreferences(ctx *beegoctx.Context) {
	status, body := c.server.ConnectorPreferences(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) workspaceList(ctx *beegoctx.Context) {
	status, body := c.server.WorkspaceList(ctx.Input.Param(":id"), requestBody(ctx))
	writeJSON(ctx, status, body)
}

func (c *Controller) projectList(ctx *beegoctx.Context) {
	status, body := c.server.ProjectList()
	writeJSON(ctx, status, body)
}

func (c *Controller) projectPatch(ctx *beegoctx.Context) {
	status, body := c.server.PatchProject(ctx.Input.Param(":id"), requestBody(ctx))
	writeJSON(ctx, status, body)
}

func (c *Controller) projectDelete(ctx *beegoctx.Context) {
	status, body := c.server.DeleteProject(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) projectSessions(ctx *beegoctx.Context) {
	status, body := c.server.ProjectSessions(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) createProject(ctx *beegoctx.Context) {
	status, body := c.server.CreateProject(requestBody(ctx))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionList(ctx *beegoctx.Context) {
	status, body := c.server.SessionList()
	writeJSON(ctx, status, body)
}

func (c *Controller) createAndStart(ctx *beegoctx.Context) {
	status, body := c.server.CreateAndStart(requestBody(ctx))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionMeta(ctx *beegoctx.Context) {
	status, body := c.server.SessionMeta(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionMetaPatch(ctx *beegoctx.Context) {
	status, body := c.server.SessionMetaPatch(ctx.Input.Param(":id"), requestBody(ctx))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionTakeover(ctx *beegoctx.Context) {
	status, body := c.server.SessionTakeover(ctx.Input.Param(":id"), ctx.Request.Method == http.MethodPost)
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionSync(ctx *beegoctx.Context) {
	status, body := c.server.SessionSync(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionDetail(ctx *beegoctx.Context) {
	status, body := c.server.SessionDetail(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionTimeline(ctx *beegoctx.Context) {
	status, body := c.server.SessionTimeline(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionSnapshot(ctx *beegoctx.Context) {
	status, body := c.server.SessionSnapshot(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionEvents(ctx *beegoctx.Context) {
	status, body := c.server.SessionEvents(ctx.Input.Param(":id"), ctx.Input.Query("after"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionRuntimeCapabilities(ctx *beegoctx.Context) {
	status, body := c.server.SessionRuntimeCapabilities(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionRuntimeState(ctx *beegoctx.Context) {
	status, body := c.server.SessionRuntimeState(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionRuntimeNotices(ctx *beegoctx.Context) {
	status, body := c.server.SessionNotices(ctx.Input.Param(":id"))
	writeJSON(ctx, status, body)
}

func (c *Controller) respondInteraction(ctx *beegoctx.Context) {
	status, body := c.server.RespondInteraction(ctx.Input.Param(":id"), ctx.Input.Param(":noticeId"), requestBody(ctx))
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionModelCatalog(ctx *beegoctx.Context) {
	status, body := c.server.SessionRuntimeCatalog(ctx.Input.Param(":id"), "catalog.listModels")
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionPermissionCatalog(ctx *beegoctx.Context) {
	status, body := c.server.SessionRuntimeCatalog(ctx.Input.Param(":id"), "catalog.listPermissions")
	writeJSON(ctx, status, body)
}

func (c *Controller) sessionRuntimeAction(ctx *beegoctx.Context, method string) {
	status, body := c.server.SessionRuntimeAction(ctx.Input.Param(":id"), method, requestBody(ctx))
	writeJSON(ctx, status, body)
}

// sessionBulk decodes the id list the batch endpoints carry.
func (c *Controller) sessionBulk(ctx *beegoctx.Context, action string) {
	var ids []string
	body, _ := io.ReadAll(ctx.Request.Body)
	_ = json.Unmarshal(body, &ids)
	status, response := c.server.SessionBulk(ids, action)
	writeJSON(ctx, status, response)
}

func (c *Controller) forwardSession(ctx *beegoctx.Context, method string) {
	status, body := c.server.ForwardSession(ctx.Input.Param(":id"), method, requestBody(ctx))
	writeJSON(ctx, status, body)
}

func (c *Controller) uploadAttachment(ctx *beegoctx.Context) {
	status, body := c.server.SaveAttachment(ctx.Input.Param(":fileId"), ctx.Input.RequestBody)
	writeJSON(ctx, status, body)
}

func (c *Controller) downloadAttachment(ctx *beegoctx.Context) {
	path, err := c.server.AttachmentPath(ctx.Input.Param(":fileId"))
	if err != nil {
		writeJSON(ctx, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	http.ServeFile(ctx.ResponseWriter, ctx.Request, path)
}
