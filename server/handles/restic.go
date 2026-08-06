package handles

import (
	"github.com/OpenListTeam/OpenList/v4/internal/resticquota"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

func ResticUsage(c *gin.Context) {
	usage, err := resticquota.Snapshot()
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, usage)
}
