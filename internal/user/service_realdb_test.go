package user

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func newTestUser(email string) *User {
	return &User{
		Email:    email,
		Password: "hashed-password",
	}
}

func TestRealDB_User_Create(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	created, err := svc.Create(newTestUser(email))

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, email, created.Email)

	var fetched User
	require.NoError(t, db.First(&fetched, "id = ?", created.ID).Error)
	assert.Equal(t, email, fetched.Email)
}

func TestRealDB_User_FindByEmail_Found(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	created, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	found, err := svc.FindByEmail(email)

	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, email, found.Email)
}

func TestRealDB_User_FindByEmail_NotFound(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	found, err := svc.FindByEmail("nonexistent-" + uuid.New().String() + "@example.com")

	assert.NoError(t, err)
	assert.Nil(t, found)
}

func TestRealDB_User_FindById_Found(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	created, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	found, err := svc.FindById(created.ID)

	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, email, found.Email)
}

func TestRealDB_User_FindById_NotFound(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	found, err := svc.FindById(uuid.New().String())

	assert.NoError(t, err)
	assert.Nil(t, found)
}

func TestRealDB_User_UpdateProfile(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	created, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	firstName := "Jane"
	lastName := "Doe"
	updated, err := svc.UpdateProfile(created.ID, UpdateProfileDto{
		FirstName: &firstName,
		LastName:  &lastName,
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, firstName, *updated.FirstName)
	assert.Equal(t, lastName, *updated.LastName)
	assert.Equal(t, email, updated.Email)

	var fetched User
	require.NoError(t, db.First(&fetched, "id = ?", created.ID).Error)
	assert.Equal(t, firstName, *fetched.FirstName)
	assert.Equal(t, lastName, *fetched.LastName)
	assert.Equal(t, email, fetched.Email)
}

func TestRealDB_User_GetProfile(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	created, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	profile, err := svc.GetProfile(created.ID)

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, created.ID, profile.ID)
	assert.Equal(t, email, profile.Email)
}

func TestRealDB_User_GetProfile_NotFound(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	profile, err := svc.GetProfile(uuid.New().String())

	require.Error(t, err)
	assert.Nil(t, profile)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}
