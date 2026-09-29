package httpapi

import (
	"log"

	beegoctx "github.com/beego/beego/v2/server/web/context"
)

func (c *Controller) connectorWS(ctx *beegoctx.Context) {
	ws, err := upgrader.Upgrade(ctx.ResponseWriter, ctx.Request, nil)
	if err != nil {
		// A failed upgrade is silent to the connector beyond "bad handshake", so
		// record what actually arrived to separate a stripped header from a
		// rejected request.
		log.Printf("connector websocket upgrade failed: %v (upgrade=%q connection=%q remote=%s)",
			err, ctx.Request.Header.Get("Upgrade"), ctx.Request.Header.Get("Connection"), ctx.Request.RemoteAddr)
		return
	}
	c.server.ServeConnector(ws)
}

func (c *Controller) clientEvents(ctx *beegoctx.Context) {
	ws, err := upgrader.Upgrade(ctx.ResponseWriter, ctx.Request, nil)
	if err != nil {
		return
	}
	c.server.ServeClientEvents(ws)
}

func (c *Controller) dashboardWS(ctx *beegoctx.Context) {
	ws, err := upgrader.Upgrade(ctx.ResponseWriter, ctx.Request, nil)
	if err != nil {
		return
	}
	c.server.ServeDashboard(ws)
}

func (c *Controller) sessionWS(ctx *beegoctx.Context) {
	id := ctx.Input.Param(":id")
	if !c.server.SessionExists(id) {
		return
	}
	ws, err := upgrader.Upgrade(ctx.ResponseWriter, ctx.Request, nil)
	if err != nil {
		return
	}
	c.server.ServeSession(id, ctx.Input.Query("clientId"), ws)
}

func (c *Controller) terminalRelay(ctx *beegoctx.Context) {
	ws, err := upgrader.Upgrade(ctx.ResponseWriter, ctx.Request, nil)
	if err != nil {
		return
	}
	c.server.ServeTerminalRelay(ctx.Input.Param(":terminalId"), ws)
}
