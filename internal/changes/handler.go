package changes

import (
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/sync", auth)
	g.GET("/changes", h.changes)
}

type changesQuery struct {
	Cursor int64 `form:"cursor" binding:"omitempty,min=0"`
	Limit  int   `form:"limit"  binding:"omitempty,min=1,max=1000"`
}

// @Summary		Pull changes
// @Description	Rows changed after the cursor across all synced entities, in seq order. Repeat with the returned cursor while hasMore is true. 409 means the cursor is ahead of the server: pull again from 0.
// @Tags		sync
// @Produce		json
// @Param		cursor query integer false "Last cursor received (0 for a full pull)" default(0)
// @Param		limit  query integer false "Max rows per page" default(500)
// @Success		200 {object} Response
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		409 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/sync/changes [get]
func (h *Handler) changes(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var q changesQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}

	resp, err := h.svc.Changes(claims.Sub, q.Cursor, q.Limit)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, resp)
}
