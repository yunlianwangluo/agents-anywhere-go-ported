package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"aa-server/internal/logic"
	"aa-server/internal/view"
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
	query := logic.TimelineQuery{
		Mode:           ctx.Input.Query("mode"),
		Limit:          queryInt(ctx, "limit"),
		AfterSeq:       queryInt(ctx, "afterSeq"),
		BeforeOrderSeq: queryInt(ctx, "beforeOrderSeq"),
	}
	status, body := c.server.SessionTimeline(ctx.Input.Param(":id"), query)
	writeJSON(ctx, status, body)
}

// queryInt reads an integer query parameter, defaulting to zero.
func queryInt(ctx *beegoctx.Context, name string) int {
	value, err := strconv.Atoi(ctx.Input.Query(name))
	if err != nil {
		return 0
	}
	return value
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
	inputs, err := readUploads(ctx)
	if err != nil {
		writeJSON(ctx, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	status, body := c.server.SaveAttachments(ctx.Input.Param(":id"), inputs)
	writeJSON(ctx, status, body)
}

func (c *Controller) downloadAttachment(ctx *beegoctx.Context) {
	status, body := c.server.AttachmentDownload(ctx.Input.Param(":fileId"))
	writeJSON(ctx, status, body)
}

// connectorAttachmentContent streams the raw bytes to the connector, which
// stages them into the bridge directory before a turn can reference them.
func (c *Controller) connectorAttachmentContent(ctx *beegoctx.Context) {
	meta, data, err := c.server.AttachmentBlob(ctx.Input.Param(":id"), ctx.Input.Param(":fileId"))
	if err != nil {
		writeJSON(ctx, http.StatusNotFound, map[string]any{"error": "attachment not found"})
		return
	}
	ctx.Output.Header("Content-Type", view.FirstNonEmpty(meta.MediaType, "application/octet-stream"))
	ctx.Output.Header("X-File-Name", meta.Name)
	ctx.Output.Header("X-File-Sha256", meta.SHA256)
	_ = ctx.Output.Body(data)
}

// readUploads reads the multipart payload the client sends. A raw body is
// accepted as well, so a plain command-line upload still works.
func readUploads(ctx *beegoctx.Context) ([]logic.AttachmentInput, error) {
	if strings.HasPrefix(ctx.Request.Header.Get("Content-Type"), "multipart/form-data") {
		if err := ctx.Request.ParseMultipartForm(32 << 20); err != nil {
			return nil, err
		}
		inputs := make([]logic.AttachmentInput, 0)
		for _, headers := range ctx.Request.MultipartForm.File {
			for _, header := range headers {
				file, err := header.Open()
				if err != nil {
					return nil, err
				}
				data, err := io.ReadAll(io.LimitReader(file, 64<<20))
				_ = file.Close()
				if err != nil {
					return nil, err
				}
				inputs = append(inputs, logic.AttachmentInput{
					Name:      header.Filename,
					MediaType: header.Header.Get("Content-Type"),
					Data:      data,
				})
			}
		}
		return inputs, nil
	}
	data := ctx.Input.RequestBody
	if len(data) == 0 {
		return nil, nil
	}
	return []logic.AttachmentInput{{Name: "upload", MediaType: ctx.Request.Header.Get("Content-Type"), Data: data}}, nil
}
