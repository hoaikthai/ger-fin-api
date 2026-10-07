package category

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
	g := rg.Group("/categories", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

// @Summary		Create category
// @Description	Create a custom transaction category (max 2 nesting levels)
// @Tags		categories
// @Accept		json
// @Produce		json
// @Param		body body CreateCategoryDto true "Category details"
// @Success		201 {object} Category
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		409 {object} common.ErrorResponse
// @Failure		410 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/categories [post]
func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateCategoryDto
	if !common.BindJSON(c, &dto) {
		return
	}

	cat, err := h.svc.Create(dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(201, cat)
}

// @Summary		List categories
// @Description	Get all categories visible to the user (defaults + their own). Filter by type with ?type=income|expense
// @Tags		categories
// @Produce		json
// @Param		type query string false "Transaction type filter" Enums(income, expense)
// @Success		200 {array} Category
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/categories [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	txType := c.Query("type")
	if txType != "" {
		if txType != string(common.TransactionTypeIncome) && txType != string(common.TransactionTypeExpense) {
			common.RespondError(c, common.NewBadRequest("type must be one of: income, expense"))
			return
		}
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

// @Summary		Get category
// @Description	Get a single category by ID
// @Tags		categories
// @Produce		json
// @Param		id path string true "Category ID"
// @Success		200 {object} Category
// @Failure		401 {object} common.ErrorResponse
// @Failure		403 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/categories/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	cat, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, cat)
}

// @Summary		Update category
// @Description	Update a user-owned category (default categories cannot be updated)
// @Tags		categories
// @Accept		json
// @Produce		json
// @Param		id   path string           true "Category ID"
// @Param		body body UpdateCategoryDto true "Fields to update"
// @Success		200 {object} Category
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		403 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Failure		410 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/categories/{id} [patch]
func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateCategoryDto
	if !common.BindJSON(c, &dto) {
		return
	}

	cat, err := h.svc.Update(c.Param("id"), dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, cat)
}

// @Summary		Delete category
// @Description	Delete a user-owned category (cannot delete if it has active transactions or child categories)
// @Tags		categories
// @Produce		json
// @Param		id path string true "Category ID"
// @Success		200 {object} common.MessageResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		403 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/categories/{id} [delete]
func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	if err := h.svc.Remove(c.Param("id"), claims.Sub); err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "Category deleted successfully"})
}
