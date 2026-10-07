package category

import (
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

// Regression: Children of shared default categories must be filtered to the
// caller's own (and default) categories, or other users' children leak.
func TestFindAll_ChildrenPreloadScopedToUser(t *testing.T) {
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	svc := NewService(db)

	const userId = "11111111-1111-1111-1111-111111111111"
	const parentId = "22222222-2222-2222-2222-222222222222"
	now := time.Now()

	mock.ExpectQuery(`SELECT \* FROM "category" WHERE \("userId" = \$1 OR "isDefault" = \$2\)`).
		WithArgs(userId, true).
		WillReturnRows(sqlmock.NewRows(testutil.CategoryCols()).
			AddRow(parentId, now, now, nil, "Food", "expense", true, nil, nil))
	mock.ExpectQuery(`SELECT \* FROM "category" WHERE "category"\."parentId" = \$1 AND \("userId" = \$2 OR "isDefault" = \$3\)`).
		WithArgs(parentId, userId, true).
		WillReturnRows(sqlmock.NewRows(testutil.CategoryCols()))

	_, err = svc.FindAll(userId)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
