package budget

import (
	"errors"
	"log"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ger_fin_api/internal/common"
)

// CurrencyConverter converts an amount between currencies (satisfied by *currency.Service).
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// BaseCurrencyProvider resolves a user's base currency (satisfied by *user.Service).
type BaseCurrencyProvider interface {
	GetBaseCurrency(userId string) (string, error)
}

type Service struct {
	db       *gorm.DB
	conv     CurrencyConverter
	settings BaseCurrencyProvider
}

func NewService(db *gorm.DB, conv CurrencyConverter, settings BaseCurrencyProvider) *Service {
	return &Service{db: db, conv: conv, settings: settings}
}

func (s *Service) Create(dto CreateBudgetDto, userId string) (*Budget, error) {
	if replay, err := common.CreateReplay(s.db, "budget", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.findOneRaw(*dto.ID, userId)
	}
	if err := s.validateCategory(dto.CategoryId, userId); err != nil {
		return nil, err
	}

	start, end := periodBounds(string(dto.PeriodType), time.Now().UTC())

	if err := s.assertNoConflict(userId, dto.CategoryId, string(dto.PeriodType), start); err != nil {
		return nil, err
	}

	baseCurrency, err := s.settings.GetBaseCurrency(userId)
	if err != nil || baseCurrency == "" {
		baseCurrency = "USD"
	}

	rollover := false
	if dto.Rollover != nil {
		rollover = *dto.Rollover
	}

	b := &Budget{
		SeriesId:    uuid.New().String(),
		CategoryId:  dto.CategoryId,
		UserId:      userId,
		PeriodType:  string(dto.PeriodType),
		PeriodStart: start,
		PeriodEnd:   end,
		Amount:      dto.Amount,
		Currency:    baseCurrency,
		CarriedOver: 0,
		Rollover:    rollover,
		IsActive:    true,
	}
	b.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		b.ID = *dto.ID
	}
	if err := s.db.Create(b).Error; err != nil {
		if common.IsUniqueViolation(err) {
			return nil, common.NewConflict("A budget already exists for this category and period")
		}
		return nil, err
	}
	return b, nil
}

// validateCategory ensures a non-nil categoryId refers to an expense category
// owned by the user (or a default category). nil categoryId (overall budget) is allowed.
func (s *Service) validateCategory(categoryId *string, userId string) error {
	if categoryId == nil {
		return nil
	}
	var row struct {
		Type      string  `gorm:"column:type"`
		IsDefault bool    `gorm:"column:isDefault"`
		UserId    *string `gorm:"column:userId"`
	}
	err := s.db.Raw(`
		SELECT "type", "isDefault", "userId"
		FROM "category" WHERE "id" = ? AND "deletedAt" IS NULL
	`, *categoryId).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.Type == "" {
		return common.NewNotFound("Category not found")
	}
	if !row.IsDefault && (row.UserId == nil || *row.UserId != userId) {
		return common.NewForbidden("Access denied to this category")
	}
	if row.Type != string(common.TransactionTypeExpense) {
		return common.NewBadRequest("Budgets can only target expense categories")
	}
	return nil
}

func (s *Service) assertNoConflict(userId string, categoryId *string, periodType string, start time.Time) error {
	q := s.db.Model(&Budget{}).
		Where(`"userId" = ? AND "periodType" = ? AND "periodStart" = ?`, userId, periodType, start)
	if categoryId == nil {
		q = q.Where(`"categoryId" IS NULL`)
	} else {
		q = q.Where(`"categoryId" = ?`, *categoryId)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return common.NewConflict("A budget already exists for this category and period")
	}
	return nil
}

type spendRow struct {
	AccountCurrency string  `gorm:"column:account_currency"`
	Total           float64 `gorm:"column:total"`
}

// computeSpent returns positive spend within [start, end] (end inclusive),
// converted to target currency. Rate-unavailable rows are excluded (logged),
// matching report/service.go.
func (s *Service) computeSpent(userId string, categoryId *string, start, end time.Time, target string) (float64, error) {
	upper := end.AddDate(0, 0, 1) // exclusive upper bound: include all of the last day

	q := `
		SELECT a."currency" AS account_currency, SUM(t."amount") AS total
		FROM "transaction" t
		JOIN "account" a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."type" = 'expense'
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" < ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL`
	args := []interface{}{userId, start, upper}

	if categoryId != nil {
		q += ` AND t."categoryId" IN (SELECT "id" FROM "category" WHERE "id" = ? OR "parentId" = ?)`
		args = append(args, *categoryId, *categoryId)
	}
	q += ` GROUP BY a."currency"`

	var rows []spendRow
	if err := s.db.Raw(q, args...).Scan(&rows).Error; err != nil {
		return 0, err
	}

	var spent float64
	for _, r := range rows {
		amt := math.Abs(r.Total)
		if s.conv != nil {
			c, err := s.conv.Convert(amt, r.AccountCurrency, target)
			if err != nil {
				log.Printf("budget: spend skipping row (currency %s -> %s): %v", r.AccountCurrency, target, err)
				continue
			}
			amt = c
		}
		spent += amt
	}
	return spent, nil
}

func (s *Service) progressFor(b Budget) (BudgetProgress, error) {
	spent, err := s.computeSpent(b.UserId, b.CategoryId, b.PeriodStart, b.PeriodEnd, b.Currency)
	if err != nil {
		return BudgetProgress{}, err
	}
	effective := b.Amount + b.CarriedOver
	var pct float64
	if effective > 0 {
		pct = math.Round((spent/effective)*10000) / 100
	}
	return BudgetProgress{
		EffectiveCap: effective,
		Spent:        spent,
		Remaining:    effective - spent,
		PercentUsed:  pct,
		IsOverBudget: spent > effective,
	}, nil
}

func (s *Service) findOneRaw(id, userId string) (*Budget, error) {
	var b Budget
	err := s.db.Preload("Category").
		Where(`"id" = ? AND "userId" = ?`, id, userId).
		First(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewNotFound("Budget not found")
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Service) FindOne(id, userId string) (*BudgetDetailResponse, error) {
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return nil, err
	}
	prog, err := s.progressFor(*b)
	if err != nil {
		return nil, err
	}
	return &BudgetDetailResponse{Budget: *b, Progress: prog}, nil
}

// FindAll returns active current-period instances: monthly budgets whose period
// starts at monthStart, plus yearly budgets whose period starts at yearStart.
func (s *Service) FindAll(userId string, monthStart, yearStart time.Time) ([]Budget, error) {
	var budgets []Budget
	err := s.db.Preload("Category").
		Where(`"userId" = ? AND "isActive" = ?`, userId, true).
		Where(`("periodType" = ? AND "periodStart" = ?) OR ("periodType" = ? AND "periodStart" = ?)`,
			string(common.BudgetPeriodMonthly), monthStart,
			string(common.BudgetPeriodYearly), yearStart).
		Order(`"createdAt" DESC`).
		Find(&budgets).Error
	return budgets, err
}

func (s *Service) Update(id string, dto UpdateBudgetDto, userId string) (*Budget, error) {
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "budget", id, userId, err)
	}
	if common.IsStale(b.ClientModifiedAt, dto.ClientModifiedAt) {
		return b, nil
	}
	if dto.ClientModifiedAt != nil {
		b.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}
	if dto.Amount != nil {
		b.Amount = *dto.Amount
	}
	if dto.Rollover != nil {
		b.Rollover = *dto.Rollover
	}
	if dto.IsActive != nil {
		b.IsActive = *dto.IsActive
	}
	if err := s.db.Save(b).Error; err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) Remove(id, userId string) error {
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "budget", id, userId, err)
	}
	return s.db.Delete(b).Error
}

// ProcessDueBudgetRollovers advances each active series up to the current period.
func (s *Service) ProcessDueBudgetRollovers() error {
	now := time.Now().UTC()

	// Latest active, non-deleted instance per series (the "tail").
	var tails []Budget
	err := s.db.Raw(`
		SELECT b.* FROM "budget" b
		WHERE b."isActive" = true AND b."deletedAt" IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM "budget" b2
			WHERE b2."seriesId" = b."seriesId"
			  AND b2."deletedAt" IS NULL
			  AND b2."isActive" = true
			  AND b2."periodStart" > b."periodStart"
		  )
	`).Scan(&tails).Error
	if err != nil {
		return err
	}

	for _, tail := range tails {
		if err := s.rollSeriesForward(tail, now); err != nil {
			log.Printf("budget: failed to roll series %s forward: %v", tail.SeriesId, err)
		}
	}
	return nil
}

func (s *Service) rollSeriesForward(tail Budget, now time.Time) error {
	targetStart, _ := periodBounds(tail.PeriodType, now)
	cur := tail

	for cur.PeriodStart.Before(targetStart) {
		prevSpent, err := s.computeSpent(cur.UserId, cur.CategoryId, cur.PeriodStart, cur.PeriodEnd, cur.Currency)
		if err != nil {
			return err
		}

		// rollover computed in the tail's currency
		carried := 0.0
		if cur.Rollover {
			carried = math.Max(0, (cur.Amount+cur.CarriedOver)-prevSpent)
		}

		amount := cur.Amount
		currency := cur.Currency

		// If base currency changed, restate the whole instance in the new currency.
		base, settingsErr := s.settings.GetBaseCurrency(cur.UserId)
		if settingsErr != nil {
			log.Printf("budget: series %s keeping currency %s (settings lookup failed: %v)", cur.SeriesId, cur.Currency, settingsErr)
		}
		if base != "" && base != cur.Currency && s.conv != nil {
			convAmount, aErr := s.conv.Convert(amount, cur.Currency, base)
			convCarried := 0.0
			cErr := error(nil)
			if carried > 0 {
				convCarried, cErr = s.conv.Convert(carried, cur.Currency, base)
			}
			if aErr == nil && cErr == nil {
				amount = convAmount
				carried = convCarried
				currency = base
			} else {
				log.Printf("budget: series %s keeping currency %s (rate to %s unavailable)", cur.SeriesId, cur.Currency, base)
			}
		}

		nextStart := nextPeriodStart(cur.PeriodType, cur.PeriodStart)
		_, nextEnd := periodBounds(cur.PeriodType, nextStart)

		next := Budget{
			SeriesId:    cur.SeriesId,
			CategoryId:  cur.CategoryId,
			UserId:      cur.UserId,
			PeriodType:  cur.PeriodType,
			PeriodStart: nextStart,
			PeriodEnd:   nextEnd,
			Amount:      amount,
			Currency:    currency,
			CarriedOver: carried,
			Rollover:    cur.Rollover,
			IsActive:    true,
		}
		if err := s.db.Create(&next).Error; err != nil {
			if common.IsUniqueViolation(err) {
				return nil // already created by a concurrent/previous run
			}
			return err
		}
		cur = next
	}
	return nil
}
