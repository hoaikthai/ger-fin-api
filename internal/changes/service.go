package changes

import (
	"database/sql"

	"ger_fin_api/internal/common"

	"gorm.io/gorm"
)

const (
	DefaultLimit = 500
	MaxLimit     = 1000
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// indexSQL lists changed rows across all synced tables in seq order. Default
// categories (userId NULL) belong to every user.
const indexSQL = `
SELECT * FROM (
	SELECT 'account' AS entity, "id", "seq", "deletedAt" IS NOT NULL AS deleted
		FROM "account" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'category', "id", "seq", "deletedAt" IS NOT NULL
		FROM "category" WHERE ("userId" = @user OR "userId" IS NULL) AND "seq" > @cursor
	UNION ALL
	SELECT 'transaction', "id", "seq", "deletedAt" IS NOT NULL
		FROM "transaction" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'budget', "id", "seq", "deletedAt" IS NOT NULL
		FROM "budget" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'recurringTransaction', "id", "seq", "deletedAt" IS NOT NULL
		FROM "recurring_transaction" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'userSettings', "id", "seq", "deletedAt" IS NOT NULL
		FROM "user_settings" WHERE "userId" = @user AND "seq" > @cursor
) AS changed
ORDER BY "seq"
LIMIT @limit`

type indexRow struct {
	Entity  string `gorm:"column:entity"`
	ID      string `gorm:"column:id"`
	Seq     int64  `gorm:"column:seq"`
	Deleted bool   `gorm:"column:deleted"`
}

// Changes returns up to limit rows changed after cursor, read from one
// consistent snapshot.
func (s *Service) Changes(userId string, cursor int64, limit int) (*Response, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	resp := newResponse(cursor)

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var latest int64
		if err := tx.Raw(`SELECT last_value FROM sync_seq`).Scan(&latest).Error; err != nil {
			return err
		}
		if cursor > latest {
			return common.NewConflict("cursor is ahead of the server; pull again from 0")
		}

		var index []indexRow
		if err := tx.Raw(indexSQL, map[string]interface{}{
			"user": userId, "cursor": cursor, "limit": limit + 1,
		}).Scan(&index).Error; err != nil {
			return err
		}
		if len(index) > limit {
			resp.HasMore = true
			index = index[:limit]
		}

		live := map[string][]string{}
		for _, row := range index {
			if row.Deleted {
				resp.Deleted = append(resp.Deleted, DeletedRef{Entity: row.Entity, ID: row.ID})
			} else {
				live[row.Entity] = append(live[row.Entity], row.ID)
			}
			resp.Cursor = row.Seq
		}
		return loadLive(tx, live, &resp.Changes)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func loadLive(tx *gorm.DB, live map[string][]string, out *ChangeSet) error {
	loads := []struct {
		entity string
		dst    interface{}
	}{
		{"account", &out.Accounts},
		{"category", &out.Categories},
		{"transaction", &out.Transactions},
		{"budget", &out.Budgets},
		{"recurringTransaction", &out.RecurringTransactions},
		{"userSettings", &out.UserSettings},
	}
	for _, l := range loads {
		ids := live[l.entity]
		if len(ids) == 0 {
			continue
		}
		if err := tx.Where(`"id" IN ?`, ids).Order(`"seq"`).Find(l.dst).Error; err != nil {
			return err
		}
	}
	return nil
}
