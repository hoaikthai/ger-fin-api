package currency

import (
	"errors"

	"ger_fin_api/internal/common"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/currencies", auth)
	g.GET("/rates", h.listRates)
	g.POST("/rates/refresh", h.refresh)
}

// @Summary     List exchange rates
// @Description Returns all currently cached exchange rates
// @Tags        currencies
// @Produce     json
// @Success     200 {array} ExchangeRate
// @Failure     401 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /currencies/rates [get]
func (h *Handler) listRates(c *gin.Context) {
	rates, err := h.svc.GetLatestRates()
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, rates)
}

// @Summary     Refresh exchange rates
// @Description Immediately re-fetches exchange rates from Open Exchange Rates
// @Tags        currencies
// @Produce     json
// @Success     200 {object} RefreshResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     500 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /currencies/rates/refresh [post]
func (h *Handler) refresh(c *gin.Context) {
	if err := h.svc.RefreshRates(); err != nil {
		if errors.Is(err, ErrRefreshTooSoon) {
			common.RespondError(c, &common.AppError{Code: 429, Msg: "Rates were refreshed recently, please try again later", ErrText: "Too Many Requests"})
			return
		}
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}
	rates, _ := h.svc.GetLatestRates()
	c.JSON(200, RefreshResponse{Message: "rates refreshed", Count: len(rates)})
}
