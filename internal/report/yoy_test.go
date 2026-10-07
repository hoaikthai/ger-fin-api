package report

import (
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

// Regression: a window whose to-month precedes its from-month (wrapping into
// the next year) used to panic in make() with a negative capacity.
func TestYearOverYear_WrappingWindowDoesNotPanic(t *testing.T) {
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	svc := NewService(db, nil)

	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"year", "month_num", "income", "expense"}))

	from := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	resp, err := svc.YearOverYear("user-1", from, to, 2, "USD")
	require.NoError(t, err)
	assert.Equal(t, []string{"2025-11", "2025-12"}, resp.Months)
}
