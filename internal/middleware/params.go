package middleware

import (
	"strings"

	"ger_fin_api/internal/common"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ValidateIDParams rejects requests whose ID path params (":id", ":jobId", …)
// are not UUIDs with a 404, instead of letting Postgres fail on the uuid cast
// and surface as a 500.
func ValidateIDParams() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, p := range c.Params {
			if p.Key != "id" && !strings.HasSuffix(p.Key, "Id") {
				continue
			}
			if _, err := uuid.Parse(p.Value); err != nil {
				common.RespondError(c, common.NewNotFound("Resource not found"))
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
