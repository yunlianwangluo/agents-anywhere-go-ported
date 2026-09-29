// Package httpapi is the routers layer: it binds the client's HTTP and
// WebSocket surface to the logic layer, authenticates callers and writes the
// responses the view layer formats.
package httpapi

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aa-server/internal/auth"
	"aa-server/internal/logic"
	"aa-server/internal/view"
	beego "github.com/beego/beego/v2/server/web"
	beegoctx "github.com/beego/beego/v2/server/web/context"
	"github.com/gorilla/websocket"
)

// Controller adapts HTTP requests onto the business layer.
type Controller struct {
	server *logic.Server
}

// NewController wires the router onto the assembled business layer.
func NewController(server *logic.Server) *Controller { return &Controller{server: server} }

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, ReadBufferSize: 64 * 1024, WriteBufferSize: 64 * 1024}

// apiController serves beego's error pages as JSON.
type apiController struct{ beego.Controller }

func (c *apiController) Health() {
	c.Data["json"] = map[string]any{"ok": true, "service": "aa-server"}
	c.ServeJSON()
}

// writeJSON is the single response writer used by every handler.
func writeJSON(ctx *beegoctx.Context, status int, value any) {
	ctx.ResponseWriter.Header().Set("Content-Type", "application/json")
	ctx.ResponseWriter.WriteHeader(status)
	_ = json.NewEncoder(ctx.ResponseWriter).Encode(value)
}

// requestBody decodes a JSON object body, degrading to an empty object.
func requestBody(ctx *beegoctx.Context) map[string]any {
	var value map[string]any
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil || json.Unmarshal(body, &value) != nil || value == nil {
		return map[string]any{}
	}
	return value
}

// auth rejects requests without the shared client key.
func (c *Controller) auth(next func(*beegoctx.Context)) func(*beegoctx.Context) {
	return func(ctx *beegoctx.Context) {
		supplied := ctx.Request.Header.Get("X-DSH-Key")
		if supplied == "" {
			supplied = strings.TrimPrefix(ctx.Request.Header.Get("Authorization"), "Bearer ")
		}
		if supplied == "" {
			supplied = ctx.Input.Query("ticket")
		}
		if !auth.Valid(c.server.ClientKey(), supplied) {
			log.Printf("request unauthorized: %s %s from %s", ctx.Request.Method, ctx.Request.URL.Path, ctx.Request.RemoteAddr)
			writeJSON(ctx, http.StatusUnauthorized, map[string]any{"detail": "unauthorized"})
			return
		}
		next(ctx)
	}
}

// requestLog records every inbound call.
func (c *Controller) requestLog(ctx *beegoctx.Context) {
	log.Printf("request: %s %s from %s", ctx.Request.Method, ctx.Request.URL.Path, ctx.Request.RemoteAddr)
}

func (c *Controller) health(ctx *beegoctx.Context) {
	writeJSON(ctx, http.StatusOK, map[string]any{"status": "ok", "version": "2.0.0", "serverTime": view.Now()})
}

func (c *Controller) authConfig(ctx *beegoctx.Context) {
	writeJSON(ctx, http.StatusOK, map[string]any{"needsBootstrap": false, "emailVerificationRequired": false, "registrationOpen": false, "oauthRegistrationOpen": false, "oauthEnabled": true, "oauthProviderLabel": "Local", "serverTime": view.Now()})
}

func (c *Controller) authMe(ctx *beegoctx.Context) {
	writeJSON(ctx, http.StatusOK, map[string]any{"userId": "local-admin", "email": nil, "emailVerified": false, "displayName": "Local Admin", "role": "admin", "disabled": false, "avatar": nil, "serverTime": view.Now()})
}

// loginParamNames are the OAuth parameters the app carries into the web session.
var loginParamNames = []string{"response_type", "client_id", "redirect_uri", "code_challenge", "code_challenge_method", "scope", "state"}

// loginPage serves the key prompt the app's web session opens. The app passes
// the OAuth parameters in the URL fragment, so the page rebuilds the request
// locally rather than depending on server-side routing of the fragment.
func (c *Controller) loginPage(ctx *beegoctx.Context) {
	c.writeLoginPage(ctx, queryParams(ctx), "")
}

// oauthAuthorize renders the same key prompt for a direct browser visit. It
// never issues a code on its own, so the key cannot be skipped.
func (c *Controller) oauthAuthorize(ctx *beegoctx.Context) {
	c.writeLoginPage(ctx, queryParams(ctx), "")
}

// oauthAuthorizeSubmit exchanges a correct key for the authorization code the
// app redeems at /oauth/token.
func (c *Controller) oauthAuthorizeSubmit(ctx *beegoctx.Context) {
	_ = ctx.Request.ParseForm()
	params := formParams(ctx.Request.Form)
	redirectURI := params["redirect_uri"]
	if redirectURI == "" {
		writeJSON(ctx, http.StatusBadRequest, map[string]any{"error": "redirect_uri is required"})
		return
	}
	redirect, err := url.Parse(redirectURI)
	if err != nil || redirect.Scheme != "agents-anywhere" {
		writeJSON(ctx, http.StatusBadRequest, map[string]any{"error": "invalid redirect_uri"})
		return
	}
	if !auth.Valid(c.server.ClientKey(), ctx.Request.Form.Get("key")) {
		log.Printf("oauth authorize rejected: %s", ctx.Request.RemoteAddr)
		c.writeLoginPage(ctx, params, "访问密钥不正确，请重试。")
		return
	}
	log.Printf("oauth authorize granted: %s", ctx.Request.RemoteAddr)
	query := redirect.Query()
	query.Set("code", c.server.ClientKey())
	if state := params["state"]; state != "" {
		query.Set("state", state)
	}
	redirect.RawQuery = query.Encode()
	http.Redirect(ctx.ResponseWriter, ctx.Request, redirect.String(), http.StatusFound)
}

// oauthToken exchanges the authorization code for the client key.
func (c *Controller) oauthToken(ctx *beegoctx.Context) {
	_ = ctx.Request.ParseForm()
	if ctx.Request.Form.Get("grant_type") != "authorization_code" || !auth.Valid(c.server.ClientKey(), ctx.Request.Form.Get("code")) {
		writeJSON(ctx, http.StatusUnauthorized, map[string]any{"error": "invalid authorization code"})
		return
	}
	writeJSON(ctx, http.StatusOK, map[string]any{"access_token": c.server.ClientKey(), "token_type": "Bearer", "expires_in": 2592000, "scope": ""})
}

// queryParams collects the OAuth parameters carried by the login URL.
func queryParams(ctx *beegoctx.Context) map[string]string {
	params := make(map[string]string, len(loginParamNames))
	for _, name := range loginParamNames {
		if value := ctx.Input.Query(name); value != "" {
			params[name] = value
		}
	}
	return params
}

// formParams collects the OAuth parameters posted back with the key.
func formParams(form url.Values) map[string]string {
	params := make(map[string]string, len(loginParamNames))
	for _, name := range loginParamNames {
		if value := form.Get(name); value != "" {
			params[name] = value
		}
	}
	return params
}

// writeLoginPage renders the key prompt, showing the error inline on a retry.
func (c *Controller) writeLoginPage(ctx *beegoctx.Context, params map[string]string, errorMessage string) {
	ctx.ResponseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := loginPageData{Params: params, Error: errorMessage, ShowForm: errorMessage != ""}
	if err := loginTemplate.Execute(ctx.ResponseWriter, data); err != nil {
		log.Printf("render login page: %v", err)
	}
}

type loginPageData struct {
	Params   map[string]string
	Error    string
	ShowForm bool
}

// loginTemplate is intentionally self-contained: the server may be reached over
// plain HTTP on a LAN where no CDN or asset host is available.
var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Agents Anywhere</title>
<style>
  :root { color-scheme: light dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
         background:#f2f2f7; font:16px/1.5 -apple-system,system-ui,"PingFang SC",sans-serif; }
  .card { box-sizing:border-box; width:min(92vw,380px); padding:24px; border-radius:16px;
          background:#fff; box-shadow:0 8px 30px rgba(0,0,0,.08); }
  h1 { margin:0 0 8px; font-size:20px; }
  p { margin:0 0 16px; font-size:14px; color:#6b6b70; }
  input[type=password] { box-sizing:border-box; width:100%; padding:12px; font-size:16px;
          border:1px solid #d1d1d6; border-radius:10px; margin-bottom:12px; }
  button { width:100%; padding:13px; font-size:16px; border:0; border-radius:10px;
          background:#0a84ff; color:#fff; }
  .error { margin:-4px 0 12px; font-size:13px; color:#ff3b30; }
  [hidden] { display:none !important; }
</style>
</head>
<body>
<main class="card">
  <h1>Agents Anywhere</h1>

  <section id="intro"{{if .ShowForm}} hidden{{end}}>
    <p>即将连接到你的服务器。点击“继续”后请输入该服务器配置的访问密钥。</p>
    <button id="continue" type="button">继续</button>
  </section>

  <form id="login" method="post" action="/api/v2/oauth/authorize" accept-charset="utf-8"{{if not .ShowForm}} hidden{{end}}>
    <p>请输入访问密钥（服务器 config.yaml 中的 client_key）。</p>
    {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    {{range $name, $value := .Params}}<input type="hidden" name="{{$name}}" value="{{$value}}">
    {{end}}<input id="key" name="key" type="password" placeholder="访问密钥" autocomplete="current-password" autocapitalize="off" required>
    <button type="submit">登录</button>
  </form>
</main>
<script>
(function () {
  var query = (location.hash.split('?')[1] || location.search.slice(1));
  var form = document.getElementById('login');
  new URLSearchParams(query).forEach(function (value, name) {
    if (form.querySelector('input[name="' + name + '"]')) return;
    var input = document.createElement('input');
    input.type = 'hidden'; input.name = name; input.value = value;
    form.appendChild(input);
  });
  document.getElementById('continue').addEventListener('click', function () {
    document.getElementById('intro').hidden = true;
    form.hidden = false;
    document.getElementById('key').focus();
  });
})();
</script>
</body>
</html>
`))

// wsTicket issues the short-lived ticket the sockets authenticate with.
func (c *Controller) wsTicket(ctx *beegoctx.Context) {
	writeJSON(ctx, http.StatusOK, map[string]any{"ticket": c.server.ClientKey(), "expiresAt": time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339), "serverTime": view.Now()})
}
