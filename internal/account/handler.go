package account

import (
	"errors"

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

// RegisterRoutes registers all account CRUD routes on the provided group.
// The GET /:id/transactions route is registered separately in the transaction
// module via RegisterAccountTransactionsRoute, since it depends on TransactionService.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/accounts", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateAccountDto
	if !common.BindJSON(c, &dto) {
		return
	}

	a, err := h.svc.Create(claims.Sub, dto)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(201, a)
}

func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	accounts, err := h.svc.FindAllByUser(claims.Sub)
	if err != nil {
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, accounts)
}

func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	id := c.Param("id")

	a, err := h.svc.FindOne(id, claims.Sub)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, a)
}

func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	id := c.Param("id")

	var dto UpdateAccountDto
	if !common.BindJSON(c, &dto) {
		return
	}

	a, err := h.svc.Update(id, claims.Sub, dto)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, a)
}

func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	id := c.Param("id")

	if err := h.svc.Remove(id, claims.Sub); err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, gin.H{"message": "Account deleted successfully"})
}
