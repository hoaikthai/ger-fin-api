package budget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"ger_fin_api/internal/common"
)

func TestPeriodBounds_Monthly(t *testing.T) {
	ref := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	start, end := periodBounds(string(common.BudgetPeriodMonthly), ref)
	assert.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), end)
}

func TestPeriodBounds_Yearly(t *testing.T) {
	ref := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	start, end := periodBounds(string(common.BudgetPeriodYearly), ref)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), end)
}

func TestNextPeriodStart(t *testing.T) {
	jun := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		nextPeriodStart(string(common.BudgetPeriodMonthly), jun))

	y2026 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		nextPeriodStart(string(common.BudgetPeriodYearly), y2026))
}
