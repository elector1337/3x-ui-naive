package controller

import (
	"net/http"
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/database/model"
	"github.com/mhsanaei/3x-ui/v3/logger"
	"github.com/mhsanaei/3x-ui/v3/web/entity"
	"github.com/mhsanaei/3x-ui/v3/web/service"

	"github.com/gin-gonic/gin"
)

type NaiveController struct {
	BaseController
	svc       *service.NaiveService
	installer *service.CaddyInstaller
}

func NewNaiveController(g *gin.RouterGroup) *NaiveController {
	a := &NaiveController{
		svc:       service.GetNaiveService(),
		installer: service.NewCaddyInstaller(),
	}
	a.initRouter(g)
	return a
}

func (a *NaiveController) initRouter(g *gin.RouterGroup) {
	g.GET("/list", a.list)
	g.GET("/get/:id", a.get)
	g.POST("/add", a.add)
	g.POST("/update/:id", a.update)
	g.POST("/delete/:id", a.delete)
	g.GET("/status/:id", a.status)
	g.POST("/start/:id", a.start)
	g.POST("/stop/:id", a.stop)
	g.POST("/restart/:id", a.restart)
	g.GET("/caddy-status", a.caddyStatus)
	g.POST("/install-caddy", a.installCaddy)
	g.POST("/preview", a.preview)
	g.POST("/validate", a.validate)
}

func parseID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return 0, false
	}
	return id, true
}

func (a *NaiveController) list(c *gin.Context) {
	rows, err := a.svc.List()
	if err != nil {
		logger.Warningf("naive list: %v", err)
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *NaiveController) get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := a.svc.Get(id)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, row, nil)
}

func (a *NaiveController) add(c *gin.Context) {
	srv := &model.NaiveServer{}
	if err := c.ShouldBind(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := a.svc.Add(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if srv.Enable {
		if err := a.svc.Start(srv.Id); err != nil {
			logger.Warningf("naive add: start failed: %v", err)
		}
	}
	jsonObj(c, srv, nil)
}

func (a *NaiveController) update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	srv := &model.NaiveServer{}
	if err := c.ShouldBind(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	srv.Id = id
	if err := a.svc.Update(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if a.svc.Status(id).Running {
		if err := a.svc.Restart(id); err != nil {
			logger.Warningf("naive update: restart failed: %v", err)
		}
	}
	jsonObj(c, srv, nil)
}

func (a *NaiveController) delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := a.svc.Delete(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "deleted", nil)
}

func (a *NaiveController) status(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	jsonObj(c, a.svc.Status(id), nil)
}

func (a *NaiveController) start(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := a.svc.Start(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "started", nil)
}

func (a *NaiveController) stop(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := a.svc.Stop(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "stopped", nil)
}

// preview renders the Caddyfile the panel would write for the given form values.
// The UI shows it next to the form so users can see exactly what runs.
func (a *NaiveController) preview(c *gin.Context) {
	srv := &model.NaiveServer{}
	if err := c.ShouldBind(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, service.RenderCaddyfile(srv), nil)
}

// validate runs `caddy adapt` on the given Caddyfile text to surface syntax errors.
func (a *NaiveController) validate(c *gin.Context) {
	var body struct {
		Text string `json:"text" form:"text"`
	}
	if err := c.ShouldBind(&body); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if err := service.ValidateCaddyfile(c.Request.Context(), body.Text); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "ok", nil)
}

func (a *NaiveController) caddyStatus(c *gin.Context) {
	jsonObj(c, a.installer.Status(), nil)
}

// installCaddy runs xcaddy to build caddy with the forward_proxy plugin.
// The build can take a couple of minutes; we stream progress as server-sent
// events so the UI can show a live log.
func (a *NaiveController) installCaddy(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	lines := make(chan string, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.installer.Install(c.Request.Context(), lines)
	}()

	for line := range lines {
		_, _ = c.Writer.Write([]byte("data: " + line + "\n\n"))
		c.Writer.Flush()
	}
	if err := <-errCh; err != nil {
		_, _ = c.Writer.Write([]byte("event: error\ndata: " + err.Error() + "\n\n"))
	} else {
		_, _ = c.Writer.Write([]byte("event: done\ndata: ok\n\n"))
	}
	c.Writer.Flush()
}

func (a *NaiveController) restart(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := a.svc.Restart(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "restarted", nil)
}
