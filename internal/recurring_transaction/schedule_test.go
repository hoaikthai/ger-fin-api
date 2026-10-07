package recurring_transaction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"ger_fin_api/internal/common"
)

func TestCalculateNextDueDate_MonthlyClampsToMonthEnd(t *testing.T) {
	d := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	var got []string
	for i := 0; i < 3; i++ {
		d = calculateNextDueDate(d, common.RecurrenceFrequencyMonthly, 31)
		got = append(got, d.Format("2006-01-02 15:04"))
	}
	assert.Equal(t, []string{"2026-02-28 09:00", "2026-03-31 09:00", "2026-04-30 09:00"}, got)
}

func TestCalculateNextDueDate_YearlyLeapDay(t *testing.T) {
	d := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	d = calculateNextDueDate(d, common.RecurrenceFrequencyYearly, 29)
	assert.Equal(t, "2025-02-28", d.Format("2006-01-02"))
	d = calculateNextDueDate(d, common.RecurrenceFrequencyYearly, 29)
	assert.Equal(t, "2026-02-28", d.Format("2006-01-02"))
}

func TestCalculateNextDueDate_DailyWeekly(t *testing.T) {
	d := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, "2026-03-02", calculateNextDueDate(d, common.RecurrenceFrequencyDaily, 1).Format("2006-01-02"))
	assert.Equal(t, "2026-03-08", calculateNextDueDate(d, common.RecurrenceFrequencyWeekly, 1).Format("2006-01-02"))
}
