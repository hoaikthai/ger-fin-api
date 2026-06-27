package recurring_transaction

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
	g := rg.Group("/recurring-transactions", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

// @Summary		Create recurring transaction
// @Description	Set up an automatic transaction that runs on a schedule (daily, weekly, monthly, or yearly)
// @Tags		recurring-transactions
// @Accept		json
// @Produce		json
// @Param		body body CreateRecurringTransactionDto true "Recurring transaction details"
// @Success		201 {object} RecurringTransaction
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/recurring-transactions [post]
func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateRecurringTransactionDto
	if !common.BindJSON(c, &dto) {
		return
	}

	rt, err := h.svc.Create(dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(201, rt)
}

// @Summary		List recurring transactions
// @Description	Get all recurring transactions for the authenticated user
// @Tags		recurring-transactions
// @Produce		json
// @Success		200 {array} RecurringTransaction
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/recurring-transactions [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	rts, err := h.svc.FindAll(claims.Sub)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, rts)
}

// @Summary		Get recurring transaction
// @Description	Get a single recurring transaction by ID
// @Tags		recurring-transactions
// @Produce		json
// @Param		id path string true "Recurring transaction ID"
// @Success		200 {object} RecurringTransaction
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/recurring-transactions/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	rt, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, rt)
}

// @Summary		Update recurring transaction
// @Description	Update a recurring transaction's settings
// @Tags		recurring-transactions
// @Accept		json
// @Produce		json
// @Param		id   path string                        true "Recurring transaction ID"
// @Param		body body UpdateRecurringTransactionDto true "Fields to update"
// @Success		200 {object} RecurringTransaction
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/recurring-transactions/{id} [patch]
func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateRecurringTransactionDto
	if !common.BindJSON(c, &dto) {
		return
	}

	rt, err := h.svc.Update(c.Param("id"), dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, rt)
}

// @Summary		Delete recurring transaction
// @Description	Soft-delete a recurring transaction by ID
// @Tags		recurring-transactions
// @Produce		json
// @Param		id path string true "Recurring transaction ID"
// @Success		200 {object} common.MessageResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/recurring-transactions/{id} [delete]
func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	if err := h.svc.Remove(c.Param("id"), claims.Sub); err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "Recurring transaction deleted successfully"})
}
