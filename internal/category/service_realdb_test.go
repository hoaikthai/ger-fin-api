package category

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func TestRealDB_Category_SeedDefault(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	err := svc.SeedDefaultCategories()
	require.NoError(t, err)

	var count int64
	err = db.Model(&Category{}).Where("\"isDefault\" = ?", true).Count(&count).Error
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(10))

	firstCount := count

	err = svc.SeedDefaultCategories()
	require.NoError(t, err)

	var countAfter int64
	err = db.Model(&Category{}).Where("\"isDefault\" = ?", true).Count(&countAfter).Error
	require.NoError(t, err)
	assert.Equal(t, firstCount, countAfter)
}

func TestRealDB_Category_Create_Success(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := uuid.New().String()

	cat, err := svc.Create(CreateCategoryDto{
		Name: "Groceries",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)
	require.NotNil(t, cat)
	assert.NotEmpty(t, cat.ID)
	assert.Equal(t, "Groceries", cat.Name)
	assert.Equal(t, string(common.TransactionTypeExpense), cat.Type)
	assert.False(t, cat.IsDefault)
	require.NotNil(t, cat.UserId)
	assert.Equal(t, userId, *cat.UserId)

	var fetched Category
	err = db.First(&fetched, "id = ?", cat.ID).Error
	require.NoError(t, err)
	assert.Equal(t, cat.Name, fetched.Name)
	assert.Equal(t, cat.Type, fetched.Type)
}

func TestRealDB_Category_Create_WithParent(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := uuid.New().String()

	parent, err := svc.Create(CreateCategoryDto{
		Name: "Transport",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	child, err := svc.Create(CreateCategoryDto{
		Name:     "Taxi",
		Type:     common.TransactionTypeExpense,
		ParentId: &parent.ID,
	}, userId)
	require.NoError(t, err)
	require.NotNil(t, child)
	require.NotNil(t, child.ParentId)
	assert.Equal(t, parent.ID, *child.ParentId)

	var fetched Category
	err = db.First(&fetched, "id = ?", child.ID).Error
	require.NoError(t, err)
	require.NotNil(t, fetched.ParentId)
	assert.Equal(t, parent.ID, *fetched.ParentId)
}

func TestRealDB_Category_Create_ParentTypeMismatch(t *testing.T) {
	svc := NewService(testutil.NewRealDB(t))
	userId := uuid.New().String()

	parent, err := svc.Create(CreateCategoryDto{
		Name: "Shopping",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	_, err = svc.Create(CreateCategoryDto{
		Name:     "Salary",
		Type:     common.TransactionTypeIncome,
		ParentId: &parent.ID,
	}, userId)
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestRealDB_Category_FindAll(t *testing.T) {
	svc := NewService(testutil.NewRealDB(t))
	userId := uuid.New().String()

	err := svc.SeedDefaultCategories()
	require.NoError(t, err)

	_, err = svc.Create(CreateCategoryDto{
		Name: "My Category",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	cats, err := svc.FindAll(userId)
	require.NoError(t, err)
	assert.Greater(t, len(cats), 1)
}

func TestRealDB_Category_FindAllByType(t *testing.T) {
	svc := NewService(testutil.NewRealDB(t))
	userId := uuid.New().String()

	err := svc.SeedDefaultCategories()
	require.NoError(t, err)

	_, err = svc.Create(CreateCategoryDto{
		Name: "My Expense",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	_, err = svc.Create(CreateCategoryDto{
		Name: "My Income",
		Type: common.TransactionTypeIncome,
	}, userId)
	require.NoError(t, err)

	cats, err := svc.FindAllByType(userId, common.TransactionTypeExpense)
	require.NoError(t, err)
	assert.NotEmpty(t, cats)
	for _, c := range cats {
		assert.Equal(t, string(common.TransactionTypeExpense), c.Type)
	}
}

func TestRealDB_Category_FindOne_Success(t *testing.T) {
	svc := NewService(testutil.NewRealDB(t))
	userId := uuid.New().String()

	created, err := svc.Create(CreateCategoryDto{
		Name: "Books",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	found, err := svc.FindOne(created.ID, userId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "Books", found.Name)
}

func TestRealDB_Category_FindOne_NotFound(t *testing.T) {
	svc := NewService(testutil.NewRealDB(t))
	userId := uuid.New().String()

	_, err := svc.FindOne(uuid.New().String(), userId)
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_Category_Update(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := uuid.New().String()

	created, err := svc.Create(CreateCategoryDto{
		Name: "Old Name",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	newName := "New Name"
	updated, err := svc.Update(created.ID, UpdateCategoryDto{Name: &newName}, userId)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "New Name", updated.Name)

	var fetched Category
	err = db.First(&fetched, "id = ?", created.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "New Name", fetched.Name)
}

func TestRealDB_Category_Remove_Success(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := uuid.New().String()

	created, err := svc.Create(CreateCategoryDto{
		Name: "To Delete",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	err = svc.Remove(created.ID, userId)
	require.NoError(t, err)

	_, err = svc.FindOne(created.ID, userId)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)

	var fetched Category
	err = db.Unscoped().First(&fetched, "id = ?", created.ID).Error
	require.NoError(t, err)
	assert.True(t, fetched.DeletedAt.Valid)
}

func TestRealDB_Category_Remove_HasChildren(t *testing.T) {
	svc := NewService(testutil.NewRealDB(t))
	userId := uuid.New().String()

	parent, err := svc.Create(CreateCategoryDto{
		Name: "Parent",
		Type: common.TransactionTypeExpense,
	}, userId)
	require.NoError(t, err)

	_, err = svc.Create(CreateCategoryDto{
		Name:     "Child",
		Type:     common.TransactionTypeExpense,
		ParentId: &parent.ID,
	}, userId)
	require.NoError(t, err)

	err = svc.Remove(parent.ID, userId)
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestRealDB_Category_Remove_DefaultForbidden(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := uuid.New().String()

	err := svc.SeedDefaultCategories()
	require.NoError(t, err)

	var defaultCat Category
	err = db.Model(&Category{}).Where("\"isDefault\" = ?", true).First(&defaultCat).Error
	require.NoError(t, err)

	err = svc.Remove(defaultCat.ID, userId)
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 403, appErr.Code)
}
