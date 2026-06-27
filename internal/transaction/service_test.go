package transaction

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
)

// ─── validateAmountSign ──────────────────────────────────────────────────────

func TestValidateAmountSign_IncomePositive(t *testing.T) {
	assert.NoError(t, validateAmountSign(common.TransactionTypeIncome, 100.0))
}

func TestValidateAmountSign_IncomeZero(t *testing.T) {
	err := validateAmountSign(common.TransactionTypeIncome, 0)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestValidateAmountSign_IncomeNegative(t *testing.T) {
	err := validateAmountSign(common.TransactionTypeIncome, -50.0)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestValidateAmountSign_ExpenseNegative(t *testing.T) {
	assert.NoError(t, validateAmountSign(common.TransactionTypeExpense, -100.0))
}

func TestValidateAmountSign_ExpenseZero(t *testing.T) {
	err := validateAmountSign(common.TransactionTypeExpense, 0)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestValidateAmountSign_ExpensePositive(t *testing.T) {
	err := validateAmountSign(common.TransactionTypeExpense, 50.0)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

// ─── getDateRange ─────────────────────────────────────────────────────────────

func TestGetDateRange_Month_CurrentPeriod(t *testing.T) {
	now := time.Now()
	result := getDateRange(common.TimePeriodMonth, 0)
	assert.Equal(t, now.Year(), result.Year())
	assert.Equal(t, now.Month(), result.Month())
	assert.Equal(t, 1, result.Day())
	assert.Equal(t, 0, result.Hour())
	assert.Equal(t, 0, result.Minute())
}

func TestGetDateRange_Month_PreviousPeriod(t *testing.T) {
	now := time.Now()
	result := getDateRange(common.TimePeriodMonth, -1)

	expected := now.AddDate(0, -1, 0)
	assert.Equal(t, expected.Year(), result.Year())
	assert.Equal(t, expected.Month(), result.Month())
	assert.Equal(t, 1, result.Day())
}

func TestGetDateRange_Year_CurrentPeriod(t *testing.T) {
	now := time.Now()
	result := getDateRange(common.TimePeriodYear, 0)
	assert.Equal(t, now.Year(), result.Year())
	assert.Equal(t, time.January, result.Month())
	assert.Equal(t, 1, result.Day())
}

func TestGetDateRange_Day_CurrentPeriod(t *testing.T) {
	now := time.Now()
	result := getDateRange(common.TimePeriodDay, 0)
	assert.Equal(t, now.Year(), result.Year())
	assert.Equal(t, now.Month(), result.Month())
	assert.Equal(t, now.Day(), result.Day())
	assert.Equal(t, 0, result.Hour())
}

func TestGetDateRange_Month_NegativeWrapAcrossYear(t *testing.T) {
	// Month offset that crosses a year boundary should not panic or return wrong year
	result := getDateRange(common.TimePeriodMonth, -13)
	assert.Equal(t, 1, result.Day())
	// Just verify it's a valid date
	assert.False(t, result.IsZero())
}

// ─── Transfer business rules ──────────────────────────────────────────────────

func TestTransfer_SameAccountReturnsError(t *testing.T) {
	svc := &Service{db: nil} // DB never reached — validation fires first

	_, err := svc.Transfer(CreateTransferDto{
		SourceAccountId:      "same-uuid",
		DestinationAccountId: "same-uuid",
		Amount:               100,
	}, "user-id")

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}
