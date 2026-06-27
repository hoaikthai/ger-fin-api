package report

import (
	"fmt"
	"strconv"
	"time"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
)

// SettingsProvider supplies per-user defaults for report parameters.
type SettingsProvider interface {
	GetReportDefaults(userId string) (cashFlowMonths, yearOverYearYears int, err error)
}

type Handler struct {
	svc      ServiceI
	settings SettingsProvider
}

func NewHandler(svc ServiceI, settings SettingsProvider) *Handler {
	return &Handler{svc: svc, settings: settings}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/reports", auth)
	g.GET("/category-breakdown", h.categoryBreakdown)
	g.GET("/monthly-trend", h.monthlyTrend)
	g.GET("/net-worth", h.netWorth)
	g.GET("/top-categories", h.topCategories)
	g.GET("/cash-flow", h.cashFlow)
	g.GET("/year-over-year", h.yearOverYear)
}

// parseDateRange returns from/to from query params, defaulting to current calendar month.
func parseDateRange(c *gin.Context) (time.Time, time.Time, error) {
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	if fromStr := c.Query("from"); fromStr != "" {
		t, err := time.Parse("2006-01-02", fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid from date: use YYYY-MM-DD")
		}
		from = t
	}
	if toStr := c.Query("to"); toStr != "" {
		t, err := time.Parse("2006-01-02", toStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid to date: use YYYY-MM-DD")
		}
		to = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.UTC)
	}
	return from, to, nil
}

// @Summary		Category breakdown
// @Description	Income and expense totals split by category for the given date range
// @Tags		reports
// @Produce		json
// @Param		from query string false "Start date (YYYY-MM-DD), defaults to first of current month"
// @Param		to   query string false "End date   (YYYY-MM-DD), defaults to today"
// @Success		200 {object} CategoryBreakdownResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/reports/category-breakdown [get]
func (h *Handler) categoryBreakdown(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}

	resp, err := h.svc.CategoryBreakdown(claims.Sub, from, to)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, resp)
}

// @Summary		Monthly trend
// @Description	Income vs expense totals grouped by month for the given date range; months with no transactions are zero-filled
// @Tags		reports
// @Produce		json
// @Param		from query string false "Start date (YYYY-MM-DD), defaults to first of current month"
// @Param		to   query string false "End date   (YYYY-MM-DD), defaults to today"
// @Success		200 {array}  MonthRow
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/reports/monthly-trend [get]
func (h *Handler) monthlyTrend(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}

	rows, err := h.svc.MonthlyTrend(claims.Sub, from, to)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, rows)
}

// @Summary		Net worth
// @Description	Sum of all active account balances for the authenticated user
// @Tags		reports
// @Produce		json
// @Success		200 {object} NetWorthResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/reports/net-worth [get]
func (h *Handler) netWorth(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	resp, err := h.svc.NetWorth(claims.Sub)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, resp)
}

// @Summary		Top spending categories
// @Description	Expense categories ranked by amount for the given date range
// @Tags		reports
// @Produce		json
// @Param		from  query string  false "Start date (YYYY-MM-DD), defaults to first of current month"
// @Param		to    query string  false "End date   (YYYY-MM-DD), defaults to today"
// @Param		limit query integer false "Number of categories to return" default(5)
// @Success		200 {array}  CategoryRow
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/reports/top-categories [get]
func (h *Handler) topCategories(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}

	limit := 5
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	rows, err := h.svc.TopCategories(claims.Sub, from, to, limit)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, rows)
}

// @Summary		Cash flow report
// @Description	Actual vs projected cash flow for a period; projection combines a rolling historical average with scheduled recurring transactions
// @Tags		reports
// @Produce		json
// @Param		from   query string  false "Start date (YYYY-MM-DD), defaults to first of current month"
// @Param		to     query string  false "End date   (YYYY-MM-DD), defaults to today"
// @Param		months query integer false "Rolling lookback months for projection (1–12, default 3)"
// @Success		200 {object} CashFlowResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/reports/cash-flow [get]
func (h *Handler) cashFlow(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}

	months := 0
	if mStr := c.Query("months"); mStr != "" {
		if m, err := strconv.Atoi(mStr); err == nil && m >= 1 && m <= 12 {
			months = m
		}
	}
	if months == 0 {
		if cfMonths, _, err := h.settings.GetReportDefaults(claims.Sub); err == nil && cfMonths > 0 {
			months = cfMonths
		} else {
			months = 3
		}
	}

	resp, err := h.svc.CashFlow(claims.Sub, from, to, months)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, resp)
}

// @Summary		Year-over-year comparison
// @Description	Monthly income and expense broken down by year for the same calendar window; months with no transactions are zero-filled
// @Tags		reports
// @Produce		json
// @Param		from  query string  false "Start date anchoring the month window (YYYY-MM-DD), defaults to first of current month"
// @Param		to    query string  false "End date anchoring the month window   (YYYY-MM-DD), defaults to today"
// @Param		years query integer false "Number of years to compare (1–5, default 2)"
// @Success		200 {object} YearOverYearResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/reports/year-over-year [get]
func (h *Handler) yearOverYear(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}

	years := 0
	if yStr := c.Query("years"); yStr != "" {
		if y, err := strconv.Atoi(yStr); err == nil && y >= 1 && y <= 5 {
			years = y
		}
	}
	if years == 0 {
		if _, yoyYears, err := h.settings.GetReportDefaults(claims.Sub); err == nil && yoyYears > 0 {
			years = yoyYears
		} else {
			years = 2
		}
	}

	resp, err := h.svc.YearOverYear(claims.Sub, from, to, years)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, resp)
}
