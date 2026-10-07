package common

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SyncFields is embedded in every model mobile clients mirror. Seq is set by a
// database trigger on every write (read-only for GORM); ClientModifiedAt is the
// device time of the latest client edit, used for last-write-wins.
type SyncFields struct {
	Seq              int64      `gorm:"column:seq;->"           json:"seq"`
	ClientModifiedAt *time.Time `gorm:"column:clientModifiedAt" json:"clientModifiedAt,omitempty"`
}

// CreateMeta lets an offline client choose the id of a new row, so retrying
// the create is safe.
type CreateMeta struct {
	ID               *string    `json:"id"               binding:"omitempty,uuid"`
	ClientModifiedAt *time.Time `json:"clientModifiedAt" binding:"omitempty"`
}

// UpdateMeta carries the device time of a client edit (last-write-wins).
type UpdateMeta struct {
	ClientModifiedAt *time.Time `json:"clientModifiedAt" binding:"omitempty"`
}

func NewGone(message string) *AppError {
	return &AppError{Code: 410, Msg: message, ErrText: "Gone"}
}

// ClientTime clamps a device timestamp to the server clock, so a device whose
// clock runs ahead cannot make its edits win over every later edit.
func ClientTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	if now := time.Now(); t.After(now) {
		return &now
	}
	return t
}

// IsStale reports whether an incoming client edit is older than the stored
// one; stale edits are dropped (last-write-wins).
func IsStale(stored, incoming *time.Time) bool {
	in := ClientTime(incoming)
	return stored != nil && in != nil && in.Before(*stored)
}

type rowState int

const (
	rowAbsent rowState = iota
	rowLive
	rowDeleted
	rowForeign
)

// lookupRow inspects a row including soft-deleted ones. table must be a
// trusted constant.
func lookupRow(db *gorm.DB, table, id, userId string) (rowState, error) {
	var rows []struct {
		UserId    *string    `gorm:"column:userId"`
		DeletedAt *time.Time `gorm:"column:deletedAt"`
	}
	if err := db.Raw(fmt.Sprintf(`SELECT "userId", "deletedAt" FROM %q WHERE "id" = ?`, table), id).
		Scan(&rows).Error; err != nil {
		return rowAbsent, err
	}
	switch {
	case len(rows) == 0:
		return rowAbsent, nil
	case rows[0].UserId == nil || *rows[0].UserId != userId:
		return rowForeign, nil
	case rows[0].DeletedAt != nil:
		return rowDeleted, nil
	default:
		return rowLive, nil
	}
}

// CreateReplay reports whether a create with this client-chosen id was
// already applied, in which case the caller returns the existing row instead
// of creating a duplicate. A deleted row answers 410 (deletes win); an id
// owned by someone else answers 409.
func CreateReplay(db *gorm.DB, table string, id *string, userId string) (bool, error) {
	if id == nil {
		return false, nil
	}
	state, err := lookupRow(db, table, *id, userId)
	if err != nil {
		return false, err
	}
	switch state {
	case rowLive:
		return true, nil
	case rowDeleted:
		return false, NewGone("This item was deleted")
	case rowForeign:
		return false, NewConflict("id is already in use")
	default:
		return false, nil
	}
}

func isNotFound(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Code == 404
}

// GoneIfDeleted turns a lookup's 404 into 410 when the user's row exists but
// is soft-deleted, so an offline edit of a deleted row is dropped by the
// client instead of retried. Other errors pass through.
func GoneIfDeleted(db *gorm.DB, table, id, userId string, err error) error {
	if !isNotFound(err) {
		return err
	}
	state, lookupErr := lookupRow(db, table, id, userId)
	if lookupErr != nil {
		return lookupErr
	}
	if state == rowDeleted {
		return NewGone("This item was deleted")
	}
	return err
}

// IgnoreIfDeleted makes a repeated delete succeed: a lookup's 404 becomes nil
// when the user's row is already soft-deleted. Other errors pass through.
func IgnoreIfDeleted(db *gorm.DB, table, id, userId string, err error) error {
	if !isNotFound(err) {
		return err
	}
	state, lookupErr := lookupRow(db, table, id, userId)
	if lookupErr != nil {
		return lookupErr
	}
	if state == rowDeleted {
		return nil
	}
	return err
}
