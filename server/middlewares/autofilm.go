package middlewares

import (
	"crypto/subtle"
	"os"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

const autoFilmServiceTokenEnvironment = "AUTOFILM_SERVICE_TOKEN"

// AutoFilmServiceAuth protects only the provider-neutral AutoFilm integration
// API. The token cannot authenticate against any other OpenList route.
func AutoFilmServiceAuth(c *gin.Context) {
	expected := strings.TrimSpace(os.Getenv(autoFilmServiceTokenEnvironment))
	if len(expected) < 32 {
		common.ErrorStrResp(c, "AutoFilm service token is not configured", 503)
		c.Abort()
		return
	}

	actual := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(actual), "bearer ") {
		actual = strings.TrimSpace(actual[7:])
	}

	if len(actual) != len(expected) ||
		subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		common.ErrorStrResp(c, "Invalid AutoFilm service token", 401)
		c.Abort()
		return
	}

	// FsUp and the standard streaming upload handler require a user in the
	// request context. This identity is scoped to this router group; the token
	// cannot reach the regular administrator API.
	admin, err := op.GetAdmin()
	if err != nil {
		common.ErrorResp(c, err, 500)
		c.Abort()
		return
	}
	common.GinAppendValues(c, conf.UserKey, admin)
	c.Next()
}
