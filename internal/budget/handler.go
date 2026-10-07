package budget

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/budgets", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

// @Summary		Create budget
// @Description	Create a monthly or yearly spending budget for an expense category (or an overall cap when categoryId is omitted)
// @Tags		budgets
// @Accept		json
// @Produce		json
// @Param		body body CreateBudgetDto true "Budget details"
// @Success		201 {object} Budget
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		403 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Failure		409 {object} common.ErrorResponse
// @Failure		410 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets [post]
func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateBudgetDto
	if !common.BindJSON(c, &dto) {
		return
	}

	b, err := h.svc.Create(dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(201, b)
}

// @Summary		List budgets
// @Description	List active budgets for the current period. Use ?period=YYYY-MM to view a past/future month and ?year=YYYY for yearly budgets.
// @Tags		budgets
// @Produce		json
// @Param		period query string false "Target month (YYYY-MM), defaults to current month"
// @Param		year   query string false "Target year (YYYY), defaults to current year"
// @Success		200 {array} Budget
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	now := time.Now().UTC()

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if p := c.Query("period"); p != "" {
		t, err := time.Parse("2006-01", p)
		if err != nil {
			common.RespondError(c, common.NewBadRequest("invalid period: use YYYY-MM"))
			return
		}
		monthStart = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}

	yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	if y := c.Query("year"); y != "" {
		yi, err := strconv.Atoi(y)
		if err != nil {
			common.RespondError(c, common.NewBadRequest("invalid year: use YYYY"))
			return
		}
		yearStart = time.Date(yi, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	budgets, err := h.svc.FindAll(claims.Sub, monthStart, yearStart)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, budgets)
}

// @Summary		Get budget with progress
// @Description	Get a single budget instance with computed spend, remaining, percent used, and over-budget flag
// @Tags		budgets
// @Produce		json
// @Param		id path string true "Budget ID"
// @Success		200 {object} BudgetDetailResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	detail, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, detail)
}

// @Summary		Update budget
// @Description	Update a budget instance's cap, rollover, or active state. Changes propagate to future periods via the roll-forward job.
// @Tags		budgets
// @Accept		json
// @Produce		json
// @Param		id   path string          true "Budget ID"
// @Param		body body UpdateBudgetDto  true "Fields to update"
// @Success		200 {object} Budget
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Failure		410 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets/{id} [patch]
func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateBudgetDto
	if !common.BindJSON(c, &dto) {
		return
	}

	b, err := h.svc.Update(c.Param("id"), dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, b)
}

// @Summary		Delete budget
// @Description	Soft-delete a budget instance. This also stops the series from rolling forward.
// @Tags		budgets
// @Produce		json
// @Param		id path string true "Budget ID"
// @Success		200 {object} common.MessageResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets/{id} [delete]
func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	if err := h.svc.Remove(c.Param("id"), claims.Sub); err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "Budget deleted successfully"})
}
