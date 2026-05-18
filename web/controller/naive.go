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

// NaiveController exposes REST endpoints for managing NaiveProxy servers
// (CRUD + start/stop/status). Routes are mounted under /panel/api/naive.
type NaiveController struct {
	BaseController
	naiveService *service.NaiveService
}

// NewNaiveController wires routes and returns the controller.
func NewNaiveController(g *gin.RouterGroup) *NaiveController {
	a := &NaiveController{naiveService: service.GetNaiveService()}
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
}

func (a *NaiveController) list(c *gin.Context) {
	rows, err := a.naiveService.List()
	if err != nil {
		logger.Warningf("naive list: %v", err)
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *NaiveController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return
	}
	row, err := a.naiveService.Get(id)
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
	if err := a.naiveService.Add(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	if srv.Enable {
		if err := a.naiveService.Start(srv.Id); err != nil {
			logger.Warningf("naive add: auto-start failed: %v", err)
		}
	}
	jsonObj(c, srv, nil)
}

func (a *NaiveController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return
	}
	srv := &model.NaiveServer{}
	if err := c.ShouldBind(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	srv.Id = id
	if err := a.naiveService.Update(srv); err != nil {
		jsonMsg(c, "", err)
		return
	}
	// reload running process to pick up the new config
	if st := a.naiveService.Status(id); st.Running {
		if err := a.naiveService.Restart(id); err != nil {
			logger.Warningf("naive update: restart failed: %v", err)
		}
	}
	jsonObj(c, srv, nil)
}

func (a *NaiveController) delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return
	}
	if err := a.naiveService.Delete(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "deleted", nil)
}

func (a *NaiveController) status(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return
	}
	jsonObj(c, a.naiveService.Status(id), nil)
}

func (a *NaiveController) start(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return
	}
	if err := a.naiveService.Start(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "started", nil)
}

func (a *NaiveController) stop(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return
	}
	if err := a.naiveService.Stop(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "stopped", nil)
}

func (a *NaiveController) restart(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: "bad id"})
		return
	}
	if err := a.naiveService.Restart(id); err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "restarted", nil)
}
