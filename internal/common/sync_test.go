package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func ptr(t time.Time) *time.Time { return &t }

func TestIsStale(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	assert.False(t, IsStale(nil, ptr(base)), "no stored edit → apply")
	assert.False(t, IsStale(ptr(base), nil), "no incoming time → apply")
	assert.True(t, IsStale(ptr(base), ptr(base.Add(-time.Second))), "older → stale")
	assert.False(t, IsStale(ptr(base), ptr(base)), "equal → apply")
	assert.False(t, IsStale(ptr(base), ptr(base.Add(time.Second))), "newer → apply")
}

func TestClientTime_ClampsFuture(t *testing.T) {
	assert.Nil(t, ClientTime(nil))
	past := time.Now().Add(-time.Minute)
	assert.Equal(t, past, *ClientTime(&past))
	future := time.Now().Add(24 * time.Hour)
	got := ClientTime(&future)
	assert.WithinDuration(t, time.Now(), *got, time.Second)
}

func TestIsStale_FutureStoredValueCannotBlockEdits(t *testing.T) {
	// Values are clamped before storing, so a stored value is never in the future.
	stored := ClientTime(ptr(time.Now().Add(48 * time.Hour)))
	assert.False(t, IsStale(stored, ptr(time.Now().Add(time.Second))))
}
