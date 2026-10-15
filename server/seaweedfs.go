package server

import (
	"net/http"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	tier "github.com/OpenListTeam/OpenList/v4/server/seaweedfs"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func SeaweedFS(g *gin.RouterGroup) {
	if !conf.Conf.SeaweedFS.Enable {
		return
	}
	h, err := tier.NewHandler(conf.Conf.SeaweedFS, db.GetDb(), op.GetStorageByMountPath)
	if err != nil {
		log.WithError(err).Error("SeaweedFS gateway configuration rejected")
		g.Any("/*path", func(c *gin.Context) { c.Status(http.StatusServiceUnavailable) })
		return
	}
	h.Register(g)
}
