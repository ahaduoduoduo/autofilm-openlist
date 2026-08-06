package server

import (
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	resticserver "github.com/OpenListTeam/OpenList/v4/server/restic"
	"github.com/gin-gonic/gin"
)

func Restic(g *gin.RouterGroup) {
	if !conf.Conf.Restic.Enable {
		g.Any("/*path", func(c *gin.Context) {
			common.ErrorStrResp(c, "Restic server is not enabled", 403)
		})
		return
	}
	handler := resticserver.NewHandler()
	g.GET("/_usage", handler.Usage)
	g.Any("/:repository", handler.Handle)
	g.Any("/:repository/*path", handler.Handle)
}
