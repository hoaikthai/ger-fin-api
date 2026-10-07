package report

import (
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"ger_fin_api/internal/account"
	recurring_transaction "ger_fin_api/internal/recurring_transaction"

	"gorm.io/gorm"
)

type ServiceI interface {
	CategoryBreakdown(userId string, from, to time.Time, baseCurrency string) (*CategoryBreakdownResponse, error)
	MonthlyTrend(userId string, from, to time.Time, baseCurrency string) (*MonthlyTrendResponse, error)
	NetWorth(userId string, baseCurrency string) (*NetWorthResponse, error)
	TopCategories(userId string, from, to time.Time, limit int, baseCurrency string) ([]CategoryRow, error)
	CashFlow(userId string, from, to time.Time, lookbackMonths int, baseCurrency string) (*CashFlowResponse, error)
	YearOverYear(userId string, from, to time.Time, years int, baseCurrency string) (*YearOverYearResponse, error)
}

// CurrencyConverter converts an amount from one currency to another.
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

type Service struct {
	db       *gorm.DB
	currency CurrencyConverter
}

func NewService(db *gorm.DB, currency CurrencyConverter) *Service {
	return &Service{db: db, currency: currency}
}

// convert converts amount into baseCurrency. ok is false when no rate is
// available; callers exclude such rows (logged) rather than mixing currencies.
func (s *Service) convert(amount float64, from, baseCurrency, context string) (float64, bool) {
	if s.currency == nil || from == baseCurrency {
		return amount, true
	}
	c, err := s.currency.Convert(amount, from, baseCurrency)
	if err != nil {
		log.Printf("report: %s skipping amount (currency %s -> %s): %v", context, from, baseCurrency, err)
		return 0, false
	}
	return c, true
}

type categoryAgg struct {
	CategoryId      string  `gorm:"column:category_id"`
	CategoryName    string  `gorm:"column:category_name"`
	TxType          string  `gorm:"column:tx_type"`
	AccountCurrency string  `gorm:"column:account_currency"`
	Total           float64 `gorm:"column:total"`
}

func (s *Service) CategoryBreakdown(userId string, from, to time.Time, baseCurrency string) (*CategoryBreakdownResponse, error) {
	var rows []categoryAgg
	err := s.db.Raw(`
		SELECT t."categoryId"  AS category_id,
		       c."name"        AS category_name,
		       t."type"        AS tx_type,
		       a."currency"    AS account_currency,
		       SUM(t."amount") AS total
		FROM "transaction" t
		JOIN "category" c ON c."id" = t."categoryId"
		JOIN "account"  a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY t."categoryId", c."name", t."type", a."currency"
		ORDER BY ABS(SUM(t."amount")) DESC
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Aggregate per category, converting to baseCurrency
	type catKey struct{ id, name, txType string }
	catTotals := make(map[catKey]float64)
	for _, r := range rows {
		key := catKey{r.CategoryId, r.CategoryName, r.TxType}
		converted := r.Total
		if s.currency != nil {
			if c, err := s.currency.Convert(r.Total, r.AccountCurrency, baseCurrency); err == nil {
				converted = c
			} else {
				log.Printf("report: CategoryBreakdown skipping row (currency %s): %v", r.AccountCurrency, err)
				continue // excluded from aggregation — rate unavailable
			}
		}
		catTotals[key] += converted
	}

	var totalIncome, totalExpense float64
	for k, total := range catTotals {
		if k.txType == "income" {
			totalIncome += total
		} else {
			totalExpense += math.Abs(total)
		}
	}

	resp := &CategoryBreakdownResponse{
		BaseCurrency: baseCurrency,
		Income:       []CategoryRow{},
		Expense:      []CategoryRow{},
	}
	for k, total := range catTotals {
		var pct float64
		if k.txType == "income" && totalIncome > 0 {
			pct = math.Round((total/totalIncome)*10000) / 100
		} else if k.txType == "expense" && totalExpense > 0 {
			pct = math.Round((math.Abs(total)/totalExpense)*10000) / 100
		}
		row := CategoryRow{
			CategoryId:   k.id,
			CategoryName: k.name,
			Total:        total,
			Percentage:   pct,
		}
		if k.txType == "income" {
			resp.Income = append(resp.Income, row)
		} else {
			resp.Expense = append(resp.Expense, row)
		}
	}
	sort.Slice(resp.Income, func(i, j int) bool { return resp.Income[i].Total > resp.Income[j].Total })
	sort.Slice(resp.Expense, func(i, j int) bool { return math.Abs(resp.Expense[i].Total) > math.Abs(resp.Expense[j].Total) })
	return resp, nil
}

type monthAgg struct {
	Month           string  `gorm:"column:month"`
	AccountCurrency string  `gorm:"column:account_currency"`
	Income          float64 `gorm:"column:income"`
	Expense         float64 `gorm:"column:expense"`
}

func (s *Service) MonthlyTrend(userId string, from, to time.Time, baseCurrency string) (*MonthlyTrendResponse, error) {
	var rows []monthAgg
	err := s.db.Raw(`
		SELECT TO_CHAR(t."transactionDate", 'YYYY-MM') AS month,
		       a."currency" AS account_currency,
		       SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
		       SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		JOIN "account" a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY TO_CHAR(t."transactionDate", 'YYYY-MM'), a."currency"
		ORDER BY month ASC
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Aggregate per month, converting to baseCurrency
	rowMap := make(map[string]MonthRow)
	for _, r := range rows {
		income := r.Income
		expense := r.Expense
		if s.currency != nil {
			incConverted, incErr := s.currency.Convert(r.Income, r.AccountCurrency, baseCurrency)
			expConverted, expErr := s.currency.Convert(r.Expense, r.AccountCurrency, baseCurrency)
			if incErr != nil || expErr != nil {
				log.Printf("report: MonthlyTrend skipping month %s row (currency %s): income_err=%v expense_err=%v", r.Month, r.AccountCurrency, incErr, expErr)
				continue // excluded — rate unavailable
			}
			income = incConverted
			expense = expConverted
		}
		prev := rowMap[r.Month]
		rowMap[r.Month] = MonthRow{
			Month:   r.Month,
			Income:  prev.Income + income,
			Expense: prev.Expense + expense,
			Net:     prev.Net + income + expense,
		}
	}

	return &MonthlyTrendResponse{
		BaseCurrency: baseCurrency,
		Months:       fillMonths(from, to, rowMap),
	}, nil
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

func (s *Service) NetWorth(userId string, baseCurrency string) (*NetWorthResponse, error) {
	var accounts []account.Account
	if err := s.db.Where(`"userId" = ? AND "isActive" = ?`, userId, true).Find(&accounts).Error; err != nil {
		return nil, err
	}

	resp := &NetWorthResponse{
		BaseCurrency: baseCurrency,
		Accounts:     make([]NetWorthAccount, 0, len(accounts)),
	}
	for _, a := range accounts {
		nwa := NetWorthAccount{
			AccountId: a.ID,
			Name:      a.Name,
			Currency:  a.Currency,
			Balance:   a.Balance,
		}
		if s.currency != nil {
			if converted, err := s.currency.Convert(a.Balance, a.Currency, baseCurrency); err == nil {
				nwa.BaseCurrencyBalance = &converted
				resp.Total += converted
			} else {
				log.Printf("report: NetWorth skipping account %s (currency %s): %v", a.ID, a.Currency, err)
				// excluded from total — rate unavailable
			}
		} else {
			resp.Total += a.Balance
		}
		resp.Accounts = append(resp.Accounts, nwa)
	}
	return resp, nil
}

func (s *Service) TopCategories(userId string, from, to time.Time, limit int, baseCurrency string) ([]CategoryRow, error) {
	if limit <= 0 {
		limit = 5
	}

	// Fetch all expense categories to compute accurate percentages before slicing to limit.
	var rows []categoryAgg
	err := s.db.Raw(`
		SELECT t."categoryId"  AS category_id,
		       c."name"        AS category_name,
		       t."type"        AS tx_type,
		       a."currency"    AS account_currency,
		       SUM(t."amount") AS total
		FROM "transaction" t
		JOIN "category" c ON c."id" = t."categoryId"
		JOIN "account"  a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."type" = 'expense'
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY t."categoryId", c."name", t."type", a."currency"
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Aggregate per category in baseCurrency.
	type catKey struct{ id, name string }
	totals := make(map[catKey]float64)
	for _, r := range rows {
		if v, ok := s.convert(r.Total, r.AccountCurrency, baseCurrency, "TopCategories"); ok {
			totals[catKey{r.CategoryId, r.CategoryName}] += v
		}
	}

	var totalExpense float64
	result := make([]CategoryRow, 0, len(totals))
	for k, total := range totals {
		totalExpense += math.Abs(total)
		result = append(result, CategoryRow{CategoryId: k.id, CategoryName: k.name, Total: total})
	}
	// Most negative (largest expense) first.
	sort.Slice(result, func(i, j int) bool { return result[i].Total < result[j].Total })

	if len(result) > limit {
		result = result[:limit]
	}
	for i := range result {
		if totalExpense > 0 {
			result[i].Percentage = math.Round((math.Abs(result[i].Total)/totalExpense)*10000) / 100
		}
	}
	return result, nil
}

type txTotals struct {
	Income  float64
	Expense float64
}

type currencyTotals struct {
	AccountCurrency string  `gorm:"column:account_currency"`
	Income          float64 `gorm:"column:income"`
	Expense         float64 `gorm:"column:expense"`
}

// sumInBaseCurrency sums income/expense over [from, to) — or [from, to] when
// inclusiveTo — converting each account currency into baseCurrency.
func (s *Service) sumInBaseCurrency(userId string, from, to time.Time, inclusiveTo bool, baseCurrency string) (txTotals, error) {
	upperOp := "<"
	if inclusiveTo {
		upperOp = "<="
	}
	var rows []currencyTotals
	err := s.db.Raw(`
		SELECT
			a."currency" AS account_currency,
			SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
			SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		JOIN "account" a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" `+upperOp+` ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY a."currency"
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return txTotals{}, err
	}
	var out txTotals
	for _, r := range rows {
		inc, incOk := s.convert(r.Income, r.AccountCurrency, baseCurrency, "CashFlow")
		exp, expOk := s.convert(r.Expense, r.AccountCurrency, baseCurrency, "CashFlow")
		if incOk && expOk {
			out.Income += inc
			out.Expense += exp
		}
	}
	return out, nil
}

func (s *Service) CashFlow(userId string, from, to time.Time, lookbackMonths int, baseCurrency string) (*CashFlowResponse, error) {
	actual, err := s.sumInBaseCurrency(userId, from, to, true, baseCurrency)
	if err != nil {
		return nil, err
	}

	lookbackFrom := from.AddDate(0, -lookbackMonths, 0)
	rolling, err := s.sumInBaseCurrency(userId, lookbackFrom, from, false, baseCurrency)
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
	if err := s.db.Preload("Account").Where(`"userId" = ? AND "isActive" = ?`, userId, true).Find(&rts).Error; err != nil {
		return nil, err
	}

	var recurIncome, recurExpense float64
	for _, rt := range rts {
		count := countOccurrences(rt.StartDate, rt.EndDate, rt.Frequency, from, to)
		if count == 0 {
			continue
		}
		amount := rt.Amount * float64(count)
		if rt.Account != nil {
			var ok bool
			if amount, ok = s.convert(amount, rt.Account.Currency, baseCurrency, "CashFlow recurring"); !ok {
				continue
			}
		}
		if rt.Type == "income" {
			recurIncome += amount
		} else {
			recurExpense -= amount
		}
	}

	projIncome := scaledIncome + recurIncome
	projExpense := scaledExpense + recurExpense

	return &CashFlowResponse{
		BaseCurrency: baseCurrency,
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
	Year            int     `gorm:"column:year"`
	MonthNum        int     `gorm:"column:month_num"`
	AccountCurrency string  `gorm:"column:account_currency"`
	Income          float64 `gorm:"column:income"`
	Expense         float64 `gorm:"column:expense"`
}

func (s *Service) YearOverYear(userId string, from, to time.Time, years int, baseCurrency string) (*YearOverYearResponse, error) {
	if years <= 0 || years > 5 {
		years = 2
	}

	fromYear := from.Year() - (years - 1)
	fromMonth := int(from.Month())
	toMonth := int(to.Month())
	// The window is a month range within one year; a range that wraps into a
	// later year (e.g. Nov → Feb) runs through December.
	if to.Year() > from.Year() || toMonth < fromMonth {
		toMonth = 12
	}

	var rows []yoyAgg
	err := s.db.Raw(`
		SELECT
			EXTRACT(YEAR  FROM t."transactionDate")::int AS year,
			EXTRACT(MONTH FROM t."transactionDate")::int AS month_num,
			a."currency" AS account_currency,
			SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
			SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		JOIN "account" a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND EXTRACT(MONTH FROM t."transactionDate") >= ?
		  AND EXTRACT(MONTH FROM t."transactionDate") <= ?
		  AND EXTRACT(YEAR  FROM t."transactionDate") >= ?
		  AND EXTRACT(YEAR  FROM t."transactionDate") <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY year, month_num, a."currency"
		ORDER BY year ASC, month_num ASC
	`, userId, fromMonth, toMonth, fromYear, from.Year()).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	lookup := make(map[int]map[int]yoyAgg)
	for _, r := range rows {
		inc, incOk := s.convert(r.Income, r.AccountCurrency, baseCurrency, "YearOverYear")
		exp, expOk := s.convert(r.Expense, r.AccountCurrency, baseCurrency, "YearOverYear")
		if !incOk || !expOk {
			continue
		}
		if _, ok := lookup[r.Year]; !ok {
			lookup[r.Year] = make(map[int]yoyAgg)
		}
		agg := lookup[r.Year][r.MonthNum]
		agg.Income += inc
		agg.Expense += exp
		lookup[r.Year][r.MonthNum] = agg
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
		BaseCurrency: baseCurrency,
		Months:       months,
		Years:  yearRows,
	}, nil
}
