package budget

import (
	"time"

	"ger_fin_api/internal/common"
)

type CreateBudgetDto struct {
	CategoryId *string             `json:"categoryId" binding:"omitempty,uuid"` // nil = overall total-expense cap
	PeriodType common.BudgetPeriod `json:"periodType" binding:"required,oneof=monthly yearly"`
	Amount     float64             `json:"amount"     binding:"required,gt=0"`
	Rollover   *bool               `json:"rollover"   binding:"omitempty"`
}

type UpdateBudgetDto struct {
	Amount   *float64 `json:"amount"   binding:"omitempty,gt=0"`
	Rollover *bool    `json:"rollover" binding:"omitempty"`
	IsActive *bool    `json:"isActive" binding:"omitempty"`
}

// BudgetProgress holds the computed analytics for a single budget instance.
type BudgetProgress struct {
	EffectiveCap float64 `json:"effectiveCap"` // amount + carriedOver
	Spent        float64 `json:"spent"`        // positive
	Remaining    float64 `json:"remaining"`    // effectiveCap - spent
	PercentUsed  float64 `json:"percentUsed"`  // 0..(>100), 2dp
	IsOverBudget bool    `json:"isOverBudget"`
}

// BudgetDetailResponse is returned by GET /budgets/:id.
type BudgetDetailResponse struct {
	Budget
	Progress BudgetProgress `json:"progress"`
}

// periodBounds returns the inclusive [start, end] calendar dates (UTC midnight)
// of the period containing ref.
func periodBounds(periodType string, ref time.Time) (time.Time, time.Time) {
	y, m, _ := ref.Date()
	if periodType == string(common.BudgetPeriodYearly) {
		start := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)
		return start, end
	}
	start := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

// nextPeriodStart returns the start date of the period following the one
// beginning at start.
func nextPeriodStart(periodType string, start time.Time) time.Time {
	if periodType == string(common.BudgetPeriodYearly) {
		return start.AddDate(1, 0, 0)
	}
	return start.AddDate(0, 1, 0)
}
