package account

import (
	"errors"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
)

// CurrencyConverter converts an amount from one currency to another.
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// BaseCurrencyProvider returns the user's preferred base currency.
type BaseCurrencyProvider interface {
	GetBaseCurrency(userId string) (string, error)
}

type Handler struct {
	svc         *Service
	currencySvc CurrencyConverter
	userSvc     BaseCurrencyProvider
}

func NewHandler(svc *Service, currencySvc CurrencyConverter, userSvc BaseCurrencyProvider) *Handler {
	return &Handler{svc: svc, currencySvc: currencySvc, userSvc: userSvc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/accounts", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

func (h *Handler) baseCurrency(userId string) string {
	if h.userSvc == nil {
		return "USD"
	}
	bc, err := h.userSvc.GetBaseCurrency(userId)
	if err != nil || bc == "" {
		return "USD"
	}
	return bc
}

func (h *Handler) toResponse(a *Account, bc string) AccountResponse {
	resp := AccountResponse{Account: *a, BaseCurrency: bc}
	if h.currencySvc != nil {
		if converted, err := h.currencySvc.Convert(a.Balance, a.Currency, bc); err == nil {
			resp.BaseCurrencyBalance = &converted
		}
	}
	return resp
}

// @Summary     Create account
// @Description Create a new financial account for the authenticated user
// @Tags        accounts
// @Accept      json
// @Produce     json
// @Param       body body CreateAccountDto true "Account details"
// @Success     201 {object} AccountResponse
// @Failure     400 {object} common.ErrorResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     409 {object} common.ErrorResponse
// @Failure     410 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts [post]
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

	bc := h.baseCurrency(claims.Sub)
	c.JSON(201, h.toResponse(a, bc))
}

// @Summary     List accounts
// @Description Get all financial accounts for the authenticated user
// @Tags        accounts
// @Produce     json
// @Success     200 {array} AccountResponse
// @Failure     401 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	accounts, err := h.svc.FindAllByUser(claims.Sub)
	if err != nil {
		common.RespondInternalError(c)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	out := make([]AccountResponse, len(accounts))
	for i := range accounts {
		out[i] = h.toResponse(&accounts[i], bc)
	}
	c.JSON(200, out)
}

// @Summary     Get account
// @Description Get a single account by ID
// @Tags        accounts
// @Produce     json
// @Param       id path string true "Account ID"
// @Success     200 {object} AccountResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     404 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts/{id} [get]
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

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(a, bc))
}

// @Summary     Update account
// @Description Update an account's details
// @Tags        accounts
// @Accept      json
// @Produce     json
// @Param       id   path string           true "Account ID"
// @Param       body body UpdateAccountDto true "Fields to update"
// @Success     200 {object} AccountResponse
// @Failure     400 {object} common.ErrorResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     404 {object} common.ErrorResponse
// @Failure     410 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts/{id} [patch]
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

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(a, bc))
}

// @Summary     Delete account
// @Description Soft-delete an account by ID
// @Tags        accounts
// @Produce     json
// @Param       id path string true "Account ID"
// @Success     200 {object} common.MessageResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     404 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts/{id} [delete]
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
