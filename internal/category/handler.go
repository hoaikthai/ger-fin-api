package category

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

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/categories", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateCategoryDto
	if !common.BindJSON(c, &dto) {
		return
	}

	cat, err := h.svc.Create(dto, claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(201, cat)
}

func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	txType := c.Query("type")
	if txType != "" {
		cats, err := h.svc.FindAllByType(claims.Sub, common.TransactionType(txType))
		if err != nil {
			common.RespondInternalError(c)
			return
		}
		c.JSON(200, cats)
		return
	}

	cats, err := h.svc.FindAll(claims.Sub)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, cats)
}

func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	cat, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(200, cat)
}

func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateCategoryDto
	if !common.BindJSON(c, &dto) {
		return
	}

	cat, err := h.svc.Update(c.Param("id"), dto, claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(200, cat)
}

func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	if err := h.svc.Remove(c.Param("id"), claims.Sub); err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "Category deleted successfully"})
}

func respondErr(c *gin.Context, err error) {
	var appErr *common.AppError
	if errors.As(err, &appErr) {
		common.RespondError(c, appErr)
		return
	}
	common.RespondInternalError(c)
}
