package auth

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

const testRefreshTTL = 90 * 24 * time.Hour

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestRefreshStore(t *testing.T) (*RefreshStore, *fakeClock, string) {
	t.Helper()
	db := testutil.NewRealDB(t)
	clock := &fakeClock{t: time.Now()}
	store := NewRefreshStore(db, testRefreshTTL)
	store.now = clock.now
	return store, clock, testutil.SeedUser(t, db)
}

func TestRealDB_RefreshStore_RotateIssuesSuccessor(t *testing.T) {
	store, _, userId := newTestRefreshStore(t)
	first, err := store.Issue(userId)
	require.NoError(t, err)

	gotUser, next, err := store.Rotate(first)
	require.NoError(t, err)
	assert.Equal(t, userId, gotUser)
	assert.NotEqual(t, first, next)

	_, _, err = store.Rotate(next)
	assert.NoError(t, err, "successor is valid")
}

func TestRealDB_RefreshStore_RejectsUnknownAndExpired(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	_, _, err := store.Rotate("not-a-token")
	assert.Equal(t, ErrInvalidRefreshToken, err)

	tok, err := store.Issue(userId)
	require.NoError(t, err)
	clock.advance(testRefreshTTL + time.Minute)
	_, _, err = store.Rotate(tok)
	assert.Equal(t, ErrInvalidRefreshToken, err)
}

func TestRealDB_RefreshStore_SlidingExpiry(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	tok, err := store.Issue(userId)
	require.NoError(t, err)

	clock.advance(80 * 24 * time.Hour)
	_, next, err := store.Rotate(tok)
	require.NoError(t, err)

	clock.advance(80 * 24 * time.Hour) // 160 days after login, 80 after last use
	_, _, err = store.Rotate(next)
	assert.NoError(t, err)
}

func TestRealDB_RefreshStore_ReuseAfterGraceRevokesFamily(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	first, err := store.Issue(userId)
	require.NoError(t, err)
	_, second, err := store.Rotate(first)
	require.NoError(t, err)

	clock.advance(reuseGrace + time.Second)
	_, _, err = store.Rotate(first)
	assert.Equal(t, ErrInvalidRefreshToken, err, "reused token rejected")

	_, _, err = store.Rotate(second)
	assert.Equal(t, ErrInvalidRefreshToken, err, "whole family revoked")
}

func TestRealDB_RefreshStore_ReuseWithinGrace(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	first, err := store.Issue(userId)
	require.NoError(t, err)
	_, second, err := store.Rotate(first) // response lost on the way to the phone
	require.NoError(t, err)

	clock.advance(5 * time.Second)
	_, retried, err := store.Rotate(first) // phone retries with the token it still has
	require.NoError(t, err)
	assert.NotEqual(t, second, retried)

	_, _, err = store.Rotate(retried)
	assert.NoError(t, err, "family not revoked")
}

func TestRealDB_RefreshStore_RevokeFamilyOf(t *testing.T) {
	store, _, userId := newTestRefreshStore(t)
	tok, err := store.Issue(userId)
	require.NoError(t, err)

	require.NoError(t, store.RevokeFamilyOf(tok, "00000000-0000-0000-0000-000000000000"))
	_, next, err := store.Rotate(tok)
	require.NoError(t, err, "another user cannot revoke it")

	require.NoError(t, store.RevokeFamilyOf(next, userId))
	_, _, err = store.Rotate(next)
	assert.Equal(t, ErrInvalidRefreshToken, err)
}

func TestRealDB_RefreshStore_RevokeWaitsForConcurrentRotation(t *testing.T) {
	db := testutil.NewRealDBShared(t)
	userId := testutil.SeedUser(t, db)
	t.Cleanup(func() { db.Exec(`DELETE FROM "user" WHERE "id" = ?`, userId) })
	store := NewRefreshStore(db, testRefreshTTL)

	original, err := store.Issue(userId)
	require.NoError(t, err)
	var familyId string
	require.NoError(t, db.Raw(`SELECT "familyId" FROM "refresh_token" WHERE "tokenHash" = ?`, hashToken(original)).Scan(&familyId).Error)

	// txB models a rotation in flight: holds the family lock, successor uncommitted.
	txB := db.Begin()
	require.NoError(t, txB.Error)
	done := false
	t.Cleanup(func() {
		if !done {
			txB.Rollback()
		}
	})
	require.NoError(t, lockFamily(txB, familyId))
	successor, err := store.insert(txB, userId, familyId)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	revoked := make(chan error, 1)
	go func() {
		defer wg.Done()
		revoked <- store.RevokeFamilyOf(original, userId)
	}()

	select {
	case err := <-revoked:
		t.Fatalf("revocation returned before the concurrent rotation committed: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	require.NoError(t, txB.Commit().Error)
	done = true
	wg.Wait()
	require.NoError(t, <-revoked)

	_, _, err = store.Rotate(successor)
	assert.Equal(t, ErrInvalidRefreshToken, err, "successor created mid-revocation is revoked")
}
