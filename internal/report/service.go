package report

import (
	"fmt"
	"math"
	"time"

	"ger_fin_api/internal/account"
	recurring_transaction "ger_fin_api/internal/recurring_transaction"

	"gorm.io/gorm"
)

type ServiceI interface {
	CategoryBreakdown(userId string, from, to time.Time) (*CategoryBreakdownResponse, error)
	MonthlyTrend(userId string, from, to time.Time) ([]MonthRow, error)
	NetWorth(userId string) (*NetWorthResponse, error)
	TopCategories(userId string, from, to time.Time, limit int) ([]CategoryRow, error)
	CashFlow(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error)
	YearOverYear(userId string, from, to time.Time, years int) (*YearOverYearResponse, error)
}

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

type categoryAgg struct {
	CategoryId   string  `gorm:"column:category_id"`
	CategoryName string  `gorm:"column:category_name"`
	TxType       string  `gorm:"column:tx_type"`
	Total        float64 `gorm:"column:total"`
}

func (s *Service) CategoryBreakdown(userId string, from, to time.Time) (*CategoryBreakdownResponse, error) {
	var rows []categoryAgg
	err := s.db.Raw(`
		SELECT t."categoryId"  AS category_id,
		       c."name"        AS category_name,
		       t."type"        AS tx_type,
		       SUM(t."amount") AS total
		FROM "transaction" t
		JOIN "category" c ON c."id" = t."categoryId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY t."categoryId", c."name", t."type"
		ORDER BY ABS(SUM(t."amount")) DESC
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	var totalIncome, totalExpense float64
	for _, r := range rows {
		if r.TxType == "income" {
			totalIncome += r.Total
		} else {
			totalExpense += math.Abs(r.Total)
		}
	}

	resp := &CategoryBreakdownResponse{
		Income:  []CategoryRow{},
		Expense: []CategoryRow{},
	}
	for _, r := range rows {
		var pct float64
		if r.TxType == "income" && totalIncome > 0 {
			pct = math.Round((r.Total/totalIncome)*10000) / 100
		} else if r.TxType == "expense" && totalExpense > 0 {
			pct = math.Round((math.Abs(r.Total)/totalExpense)*10000) / 100
		}
		row := CategoryRow{
			CategoryId:   r.CategoryId,
			CategoryName: r.CategoryName,
			Total:        r.Total,
			Percentage:   pct,
		}
		if r.TxType == "income" {
			resp.Income = append(resp.Income, row)
		} else {
			resp.Expense = append(resp.Expense, row)
		}
	}
	return resp, nil
}

type monthAgg struct {
	Month   string  `gorm:"column:month"`
	Income  float64 `gorm:"column:income"`
	Expense float64 `gorm:"column:expense"`
}

func (s *Service) MonthlyTrend(userId string, from, to time.Time) ([]MonthRow, error) {
	var rows []monthAgg
	err := s.db.Raw(`
		SELECT TO_CHAR(t."transactionDate", 'YYYY-MM') AS month,
		       SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
		       SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY TO_CHAR(t."transactionDate", 'YYYY-MM')
		ORDER BY month ASC
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	rowMap := make(map[string]MonthRow, len(rows))
	for _, r := range rows {
		rowMap[r.Month] = MonthRow{
			Month:   r.Month,
			Income:  r.Income,
			Expense: r.Expense,
			Net:     r.Income + r.Expense,
		}
	}
	return fillMonths(from, to, rowMap), nil
}

func fillMonths(from, to time.Time, data map[string]MonthRow) []MonthRow {
	var result []MonthRow
	cur := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !cur.After(end) {
		key := cur.Format("2006-01")
		if row, ok := data[key]; ok {
			result = append(result, row)
		} else {
			result = append(result, MonthRow{Month: key})
		}
		cur = cur.AddDate(0, 1, 0)
	}
	return result
}

func (s *Service) NetWorth(userId string) (*NetWorthResponse, error) {
	var accounts []account.Account
	if err := s.db.Where(`"userId" = ? AND "isActive" = ?`, userId, true).Find(&accounts).Error; err != nil {
		return nil, err
	}

	resp := &NetWorthResponse{Accounts: make([]NetWorthAccount, 0, len(accounts))}
	for _, a := range accounts {
		resp.Total += a.Balance
		resp.Accounts = append(resp.Accounts, NetWorthAccount{
			AccountId: a.ID,
			Name:      a.Name,
			Currency:  a.Currency,
			Balance:   a.Balance,
		})
	}
	return resp, nil
}

func (s *Service) TopCategories(userId string, from, to time.Time, limit int) ([]CategoryRow, error) {
	if limit <= 0 {
		limit = 5
	}

	// Fetch all expense categories to compute accurate percentages before slicing to limit.
	var rows []categoryAgg
	err := s.db.Raw(`
		SELECT t."categoryId"  AS category_id,
		       c."name"        AS category_name,
		       t."type"        AS tx_type,
		       SUM(t."amount") AS total
		FROM "transaction" t
		JOIN "category" c ON c."id" = t."categoryId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."type" = 'expense'
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY t."categoryId", c."name", t."type"
		ORDER BY SUM(t."amount") ASC
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	var totalExpense float64
	for _, r := range rows {
		totalExpense += math.Abs(r.Total)
	}

	if len(rows) > limit {
		rows = rows[:limit]
	}

	result := make([]CategoryRow, 0, len(rows))
	for _, r := range rows {
		var pct float64
		if totalExpense > 0 {
			pct = math.Round((math.Abs(r.Total)/totalExpense)*10000) / 100
		}
		result = append(result, CategoryRow{
			CategoryId:   r.CategoryId,
			CategoryName: r.CategoryName,
			Total:        r.Total,
			Percentage:   pct,
		})
	}
	return result, nil
}

type txTotals struct {
	Income  float64 `gorm:"column:income"`
	Expense float64 `gorm:"column:expense"`
}

func (s *Service) CashFlow(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error) {
	var actual txTotals
	err := s.db.Raw(`
		SELECT
			SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
			SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
	`, userId, from, to).Scan(&actual).Error
	if err != nil {
		return nil, err
	}

	lookbackFrom := from.AddDate(0, -lookbackMonths, 0)
	var rolling txTotals
	err = s.db.Raw(`
		SELECT
			SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
			SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" < ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
	`, userId, lookbackFrom, from).Scan(&rolling).Error
	if err != nil {
		return nil, err
	}

	periodDays := to.Sub(from).Hours() / 24
	if periodDays < 1 {
		periodDays = 1
	}
	scale := periodDays / 30.0
	scaledIncome := (rolling.Income / float64(lookbackMonths)) * scale
	scaledExpense := (rolling.Expense / float64(lookbackMonths)) * scale

	var rts []recurring_transaction.RecurringTransaction
	if err := s.db.Where(`"userId" = ? AND "isActive" = ?`, userId, true).Find(&rts).Error; err != nil {
		return nil, err
	}

	var recurIncome, recurExpense float64
	for _, rt := range rts {
		count := countOccurrences(rt.StartDate, rt.EndDate, rt.Frequency, from, to)
		if count == 0 {
			continue
		}
		if rt.Type == "income" {
			recurIncome += rt.Amount * float64(count)
		} else {
			recurExpense -= rt.Amount * float64(count)
		}
	}

	projIncome := scaledIncome + recurIncome
	projExpense := scaledExpense + recurExpense

	return &CashFlowResponse{
		Period: CashFlowPeriod{
			From: from.Format("2006-01-02"),
			To:   to.Format("2006-01-02"),
		},
		Actual: CashFlowSummary{
			Income:  actual.Income,
			Expense: actual.Expense,
			Net:     actual.Income + actual.Expense,
		},
		Projected: CashFlowSummary{
			Income:  projIncome,
			Expense: projExpense,
			Net:     projIncome + projExpense,
		},
		Recurring: CashFlowRecurring{
			Income:  recurIncome,
			Expense: recurExpense,
		},
	}, nil
}

func countOccurrences(startDate time.Time, endDate *time.Time, frequency string, from, to time.Time) int {
	cur := startDate
	for cur.Before(from) {
		cur = advanceByFrequency(cur, frequency)
	}
	count := 0
	for !cur.After(to) {
		if endDate != nil && cur.After(*endDate) {
			break
		}
		count++
		cur = advanceByFrequency(cur, frequency)
	}
	return count
}

func advanceByFrequency(t time.Time, frequency string) time.Time {
	switch frequency {
	case "daily":
		return t.AddDate(0, 0, 1)
	case "weekly":
		return t.AddDate(0, 0, 7)
	case "monthly":
		return t.AddDate(0, 1, 0)
	case "yearly":
		return t.AddDate(1, 0, 0)
	default:
		return t.AddDate(0, 1, 0)
	}
}

type yoyAgg struct {
	Year     int     `gorm:"column:year"`
	MonthNum int     `gorm:"column:month_num"`
	Income   float64 `gorm:"column:income"`
	Expense  float64 `gorm:"column:expense"`
}

func (s *Service) YearOverYear(userId string, from, to time.Time, years int) (*YearOverYearResponse, error) {
	if years <= 0 || years > 5 {
		years = 2
	}

	fromYear := from.Year() - (years - 1)
	fromMonth := int(from.Month())
	toMonth := int(to.Month())

	var rows []yoyAgg
	err := s.db.Raw(`
		SELECT
			EXTRACT(YEAR  FROM t."transactionDate")::int AS year,
			EXTRACT(MONTH FROM t."transactionDate")::int AS month_num,
			SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
			SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		WHERE t."userId" = ?
		  AND EXTRACT(MONTH FROM t."transactionDate") >= ?
		  AND EXTRACT(MONTH FROM t."transactionDate") <= ?
		  AND EXTRACT(YEAR  FROM t."transactionDate") >= ?
		  AND EXTRACT(YEAR  FROM t."transactionDate") <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY year, month_num
		ORDER BY year ASC, month_num ASC
	`, userId, fromMonth, toMonth, fromYear, from.Year()).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	lookup := make(map[int]map[int]yoyAgg)
	for _, r := range rows {
		if _, ok := lookup[r.Year]; !ok {
			lookup[r.Year] = make(map[int]yoyAgg)
		}
		lookup[r.Year][r.MonthNum] = r
	}

	months := make([]string, 0, toMonth-fromMonth+1)
	for m := fromMonth; m <= toMonth; m++ {
		months = append(months, fmt.Sprintf("%d-%02d", from.Year(), m))
	}

	yearRows := make([]YearRow, 0, years)
	for y := fromYear; y <= from.Year(); y++ {
		data := make([]MonthRow, 0, toMonth-fromMonth+1)
		for m := fromMonth; m <= toMonth; m++ {
			row := MonthRow{Month: fmt.Sprintf("%d-%02d", y, m)}
			if agg, ok := lookup[y][m]; ok {
				row.Income = agg.Income
				row.Expense = agg.Expense
				row.Net = agg.Income + agg.Expense
			}
			data = append(data, row)
		}
		yearRows = append(yearRows, YearRow{Year: y, Data: data})
	}

	return &YearOverYearResponse{
		Months: months,
		Years:  yearRows,
	}, nil
}
