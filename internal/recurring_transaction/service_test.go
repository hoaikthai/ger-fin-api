package recurring_transaction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

// ─── calculateNextDueDate ─────────────────────────────────────────────────────

func TestCalculateNextDueDate_Daily(t *testing.T) {
	base := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)
	result := calculateNextDueDate(base, common.RecurrenceFrequencyDaily)
	assert.Equal(t, time.Date(2024, 3, 16, 12, 0, 0, 0, time.UTC), result)
}

func TestCalculateNextDueDate_Weekly(t *testing.T) {
	base := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	result := calculateNextDueDate(base, common.RecurrenceFrequencyWeekly)
	assert.Equal(t, time.Date(2024, 3, 22, 0, 0, 0, 0, time.UTC), result)
}

func TestCalculateNextDueDate_Monthly(t *testing.T) {
	base := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	result := calculateNextDueDate(base, common.RecurrenceFrequencyMonthly)
	// Go's AddDate handles month overflow: Jan 31 + 1 month = Mar 2 (leap year 2024)
	assert.Equal(t, time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC), result)
}

func TestCalculateNextDueDate_Yearly(t *testing.T) {
	base := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC) // leap day
	result := calculateNextDueDate(base, common.RecurrenceFrequencyYearly)
	// Feb 29 + 1 year = Mar 1 2025 (Go's AddDate normalises overflow)
	assert.Equal(t, time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC), result)
}

func TestCalculateNextDueDate_Monthly_NormalDay(t *testing.T) {
	base := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	result := calculateNextDueDate(base, common.RecurrenceFrequencyMonthly)
	assert.Equal(t, time.Date(2024, 7, 15, 0, 0, 0, 0, time.UTC), result)
}

// ─── Service.Create ───────────────────────────────────────────────────────────

func newRecurringTxSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	return &Service{db: db, transactionSvc: nil}, mock
}

func TestRecurringTxCreate_SetsNextDueDateToStartDate(t *testing.T) {
	svc, mock := newRecurringTxSvc(t)

	startDate := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "recurring_transaction"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("rt-uuid"))
	mock.ExpectCommit()

	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeExpense,
		Amount:      50.0,
		Description: "Monthly rent",
		CategoryId:  "cat-uuid",
		AccountId:   "acc-uuid",
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   startDate,
	}, "user-id")

	require.NoError(t, err)
	require.NotNil(t, rt)
	require.NotNil(t, rt.NextDueDate)
	assert.Equal(t, startDate, *rt.NextDueDate)
	assert.True(t, rt.IsActive)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRecurringTxCreate_IsActiveDefaultsToTrue(t *testing.T) {
	svc, mock := newRecurringTxSvc(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "recurring_transaction"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("rt-uuid"))
	mock.ExpectCommit()

	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      200.0,
		Description: "Salary",
		CategoryId:  "cat-uuid",
		AccountId:   "acc-uuid",
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   time.Now(),
	}, "user-id")

	require.NoError(t, err)
	assert.True(t, rt.IsActive)
	assert.NoError(t, mock.ExpectationsWereMet())
}
