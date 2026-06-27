package report

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
)

// mockService is a handwritten test double for ServiceI.
type mockService struct {
	categoryBreakdownFn func(userId string, from, to time.Time) (*CategoryBreakdownResponse, error)
	monthlyTrendFn      func(userId string, from, to time.Time) ([]MonthRow, error)
	netWorthFn          func(userId string) (*NetWorthResponse, error)
	topCategoriesFn     func(userId string, from, to time.Time, limit int) ([]CategoryRow, error)
	cashFlowFn          func(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error)
	yearOverYearFn      func(userId string, from, to time.Time, years int) (*YearOverYearResponse, error)
}

func (m *mockService) CategoryBreakdown(userId string, from, to time.Time) (*CategoryBreakdownResponse, error) {
	return m.categoryBreakdownFn(userId, from, to)
}
func (m *mockService) MonthlyTrend(userId string, from, to time.Time) ([]MonthRow, error) {
	return m.monthlyTrendFn(userId, from, to)
}
func (m *mockService) NetWorth(userId string) (*NetWorthResponse, error) {
	return m.netWorthFn(userId)
}
func (m *mockService) TopCategories(userId string, from, to time.Time, limit int) ([]CategoryRow, error) {
	return m.topCategoriesFn(userId, from, to, limit)
}
func (m *mockService) CashFlow(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error) {
	return m.cashFlowFn(userId, from, to, lookbackMonths)
}
func (m *mockService) YearOverYear(userId string, from, to time.Time, years int) (*YearOverYearResponse, error) {
	return m.yearOverYearFn(userId, from, to, years)
}

// newTestRouter wires up the report handler against a fake auth middleware that
// injects the given userId into the Gin context, bypassing real JWT verification.
func newTestRouter(svc ServiceI, userId string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	fakeAuth := func(c *gin.Context) {
		c.Set("user", &common.JWTClaims{Sub: userId})
		c.Next()
	}
	h := NewHandler(svc)
	h.RegisterRoutes(r.Group(""), fakeAuth)
	return r
}

func doGet(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

// ── CategoryBreakdown ────────────────────────────────────────────────────────

func TestHandler_CategoryBreakdown_Success(t *testing.T) {
	want := &CategoryBreakdownResponse{
		Income:  []CategoryRow{{CategoryId: "c1", CategoryName: "Salary", Total: 1000, Percentage: 100}},
		Expense: []CategoryRow{{CategoryId: "c2", CategoryName: "Food", Total: -300, Percentage: 100}},
	}
	svc := &mockService{
		categoryBreakdownFn: func(userId string, from, to time.Time) (*CategoryBreakdownResponse, error) {
			assert.Equal(t, "user-1", userId)
			return want, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/category-breakdown")

	assert.Equal(t, http.StatusOK, w.Code)
	var got CategoryBreakdownResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, want.Income[0].CategoryName, got.Income[0].CategoryName)
	assert.Equal(t, want.Expense[0].Total, got.Expense[0].Total)
}

func TestHandler_CategoryBreakdown_DefaultDateRange(t *testing.T) {
	now := time.Now()
	expectedFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var capturedFrom time.Time
	svc := &mockService{
		categoryBreakdownFn: func(userId string, from, to time.Time) (*CategoryBreakdownResponse, error) {
			capturedFrom = from
			return &CategoryBreakdownResponse{Income: []CategoryRow{}, Expense: []CategoryRow{}}, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/category-breakdown")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, expectedFrom, capturedFrom)
}

func TestHandler_CategoryBreakdown_CustomDateRange(t *testing.T) {
	var capturedFrom, capturedTo time.Time
	svc := &mockService{
		categoryBreakdownFn: func(userId string, from, to time.Time) (*CategoryBreakdownResponse, error) {
			capturedFrom = from
			capturedTo = to
			return &CategoryBreakdownResponse{Income: []CategoryRow{}, Expense: []CategoryRow{}}, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/category-breakdown?from=2026-01-01&to=2026-01-31")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2026, capturedFrom.Year())
	assert.Equal(t, time.January, capturedFrom.Month())
	assert.Equal(t, 1, capturedFrom.Day())
	assert.Equal(t, 2026, capturedTo.Year())
	assert.Equal(t, time.January, capturedTo.Month())
	assert.Equal(t, 31, capturedTo.Day())
}

func TestHandler_CategoryBreakdown_InvalidDate(t *testing.T) {
	svc := &mockService{}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/category-breakdown?from=not-a-date")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_CategoryBreakdown_ServiceError(t *testing.T) {
	svc := &mockService{
		categoryBreakdownFn: func(userId string, from, to time.Time) (*CategoryBreakdownResponse, error) {
			return nil, errors.New("db error")
		},
	}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/category-breakdown")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── MonthlyTrend ─────────────────────────────────────────────────────────────

func TestHandler_MonthlyTrend_Success(t *testing.T) {
	want := []MonthRow{
		{Month: "2026-01", Income: 1000, Expense: -300, Net: 700},
		{Month: "2026-02", Income: 0, Expense: 0, Net: 0},
	}
	svc := &mockService{
		monthlyTrendFn: func(userId string, from, to time.Time) ([]MonthRow, error) {
			return want, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/monthly-trend?from=2026-01-01&to=2026-02-28")

	assert.Equal(t, http.StatusOK, w.Code)
	var got []MonthRow
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Len(t, got, 2)
	assert.Equal(t, "2026-01", got[0].Month)
	assert.Equal(t, 700.0, got[0].Net)
}

func TestHandler_MonthlyTrend_InvalidDate(t *testing.T) {
	svc := &mockService{}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/monthly-trend?to=2026/01/31")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_MonthlyTrend_ServiceError(t *testing.T) {
	svc := &mockService{
		monthlyTrendFn: func(userId string, from, to time.Time) ([]MonthRow, error) {
			return nil, errors.New("db error")
		},
	}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/monthly-trend")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── NetWorth ─────────────────────────────────────────────────────────────────

func TestHandler_NetWorth_Success(t *testing.T) {
	want := &NetWorthResponse{
		Total: 7000,
		Accounts: []NetWorthAccount{
			{AccountId: "a1", Name: "Savings", Currency: "USD", Balance: 5000},
			{AccountId: "a2", Name: "Checking", Currency: "USD", Balance: 2000},
		},
	}
	svc := &mockService{
		netWorthFn: func(userId string) (*NetWorthResponse, error) {
			assert.Equal(t, "user-1", userId)
			return want, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/net-worth")

	assert.Equal(t, http.StatusOK, w.Code)
	var got NetWorthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, 7000.0, got.Total)
	assert.Len(t, got.Accounts, 2)
}

func TestHandler_NetWorth_ServiceError(t *testing.T) {
	svc := &mockService{
		netWorthFn: func(userId string) (*NetWorthResponse, error) {
			return nil, errors.New("db error")
		},
	}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/net-worth")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── TopCategories ─────────────────────────────────────────────────────────────

func TestHandler_TopCategories_Success(t *testing.T) {
	want := []CategoryRow{
		{CategoryId: "c1", CategoryName: "Food", Total: -300, Percentage: 60},
		{CategoryId: "c2", CategoryName: "Transport", Total: -200, Percentage: 40},
	}
	svc := &mockService{
		topCategoriesFn: func(userId string, from, to time.Time, limit int) ([]CategoryRow, error) {
			assert.Equal(t, 2, limit)
			return want, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/top-categories?limit=2")

	assert.Equal(t, http.StatusOK, w.Code)
	var got []CategoryRow
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Len(t, got, 2)
	assert.Equal(t, -300.0, got[0].Total)
}

func TestHandler_TopCategories_DefaultLimit(t *testing.T) {
	var capturedLimit int
	svc := &mockService{
		topCategoriesFn: func(userId string, from, to time.Time, limit int) ([]CategoryRow, error) {
			capturedLimit = limit
			return []CategoryRow{}, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/top-categories")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 5, capturedLimit)
}

func TestHandler_TopCategories_InvalidDate(t *testing.T) {
	svc := &mockService{}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/top-categories?from=baddate")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_TopCategories_ServiceError(t *testing.T) {
	svc := &mockService{
		topCategoriesFn: func(userId string, from, to time.Time, limit int) ([]CategoryRow, error) {
			return nil, errors.New("db error")
		},
	}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/top-categories")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── CashFlow ─────────────────────────────────────────────────────────────────

func TestHandler_CashFlow_Success(t *testing.T) {
	want := &CashFlowResponse{
		Period:    CashFlowPeriod{From: "2026-06-01", To: "2026-06-30"},
		Actual:    CashFlowSummary{Income: 3200, Expense: -1800, Net: 1400},
		Projected: CashFlowSummary{Income: 3000, Expense: -2000, Net: 1000},
		Recurring: CashFlowRecurring{Income: 500, Expense: -300},
	}
	svc := &mockService{
		cashFlowFn: func(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error) {
			assert.Equal(t, "user-1", userId)
			assert.Equal(t, 3, lookbackMonths)
			return want, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/cash-flow?from=2026-06-01&to=2026-06-30")

	assert.Equal(t, http.StatusOK, w.Code)
	var got CashFlowResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, 3200.0, got.Actual.Income)
	assert.Equal(t, 1400.0, got.Actual.Net)
}

func TestHandler_CashFlow_DefaultMonths(t *testing.T) {
	var captured int
	svc := &mockService{
		cashFlowFn: func(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error) {
			captured = lookbackMonths
			return &CashFlowResponse{Period: CashFlowPeriod{}, Actual: CashFlowSummary{}, Projected: CashFlowSummary{}, Recurring: CashFlowRecurring{}}, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/cash-flow")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 3, captured)
}

func TestHandler_CashFlow_CustomMonths(t *testing.T) {
	var captured int
	svc := &mockService{
		cashFlowFn: func(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error) {
			captured = lookbackMonths
			return &CashFlowResponse{Period: CashFlowPeriod{}, Actual: CashFlowSummary{}, Projected: CashFlowSummary{}, Recurring: CashFlowRecurring{}}, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/cash-flow?months=6")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 6, captured)
}

func TestHandler_CashFlow_InvalidDate(t *testing.T) {
	svc := &mockService{}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/cash-flow?from=not-a-date")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_CashFlow_ServiceError(t *testing.T) {
	svc := &mockService{
		cashFlowFn: func(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error) {
			return nil, errors.New("db error")
		},
	}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/cash-flow")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── YearOverYear ─────────────────────────────────────────────────────────────

func TestHandler_YearOverYear_Success(t *testing.T) {
	want := &YearOverYearResponse{
		Months: []string{"2026-01", "2026-06"},
		Years: []YearRow{
			{Year: 2025, Data: []MonthRow{{Month: "2025-01", Income: 1000, Expense: -500, Net: 500}}},
			{Year: 2026, Data: []MonthRow{{Month: "2026-01", Income: 1200, Expense: -600, Net: 600}}},
		},
	}
	svc := &mockService{
		yearOverYearFn: func(userId string, from, to time.Time, years int) (*YearOverYearResponse, error) {
			assert.Equal(t, "user-1", userId)
			assert.Equal(t, 2, years)
			return want, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/year-over-year?from=2026-01-01&to=2026-06-30")

	assert.Equal(t, http.StatusOK, w.Code)
	var got YearOverYearResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Len(t, got.Years, 2)
	assert.Equal(t, 2025, got.Years[0].Year)
}

func TestHandler_YearOverYear_DefaultYears(t *testing.T) {
	var captured int
	svc := &mockService{
		yearOverYearFn: func(userId string, from, to time.Time, years int) (*YearOverYearResponse, error) {
			captured = years
			return &YearOverYearResponse{Months: []string{}, Years: []YearRow{}}, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/year-over-year")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2, captured)
}

func TestHandler_YearOverYear_CustomYears(t *testing.T) {
	var captured int
	svc := &mockService{
		yearOverYearFn: func(userId string, from, to time.Time, years int) (*YearOverYearResponse, error) {
			captured = years
			return &YearOverYearResponse{Months: []string{}, Years: []YearRow{}}, nil
		},
	}

	w := doGet(newTestRouter(svc, "user-1"), "/reports/year-over-year?years=4")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 4, captured)
}

func TestHandler_YearOverYear_InvalidDate(t *testing.T) {
	svc := &mockService{}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/year-over-year?to=2026/06/30")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_YearOverYear_ServiceError(t *testing.T) {
	svc := &mockService{
		yearOverYearFn: func(userId string, from, to time.Time, years int) (*YearOverYearResponse, error) {
			return nil, errors.New("db error")
		},
	}
	w := doGet(newTestRouter(svc, "user-1"), "/reports/year-over-year")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
