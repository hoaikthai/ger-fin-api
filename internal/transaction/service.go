package transaction

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Transfer looks up these seeded categories by name; they must match SeedDefaultCategories exactly.
const (
	transferOutCategory = "Outgoing transfer"
	transferInCategory  = "Incoming transfer"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) withRelations(q *gorm.DB) *gorm.DB {
	return q.Preload("Account").Preload("Category")
}

// getDateRange returns the start timestamp for the given period + offset,
// matching the Node implementation's behavior exactly.
func getDateRange(period common.TimePeriod, offset int) time.Time {
	now := time.Now()

	switch period {
	case common.TimePeriodDay:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return start.AddDate(0, 0, offset)

	case common.TimePeriodWeek:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		weekStart := start.AddDate(0, 0, -int(now.Weekday())) // Sunday = 0
		return weekStart.AddDate(0, 0, offset*7)

	case common.TimePeriodQuarter:
		currentQ := (int(now.Month()) - 1) / 3
		targetQ := currentQ + offset
		year := now.Year() + int(math.Floor(float64(targetQ)/4))
		adjustedQ := ((targetQ % 4) + 4) % 4
		return time.Date(year, time.Month(adjustedQ*3+1), 1, 0, 0, 0, 0, now.Location())

	case common.TimePeriodYear:
		return time.Date(now.Year()+offset, 1, 1, 0, 0, 0, 0, now.Location())

	default: // month
		month := int(now.Month()) - 1 + offset // 0-based
		year := now.Year() + int(math.Floor(float64(month)/12))
		adjustedMonth := ((month%12)+12)%12 + 1 // back to 1-based
		return time.Date(year, time.Month(adjustedMonth), 1, 0, 0, 0, 0, now.Location())
	}
}

// adjustBalance applies delta to an account's running balance. Amounts are
// signed (income > 0, expense < 0), so a transaction's effect is +amount.
func adjustBalance(db *gorm.DB, accountId string, delta float64) error {
	if delta == 0 {
		return nil
	}
	return db.Model(&account.Account{}).Where("id = ?", accountId).
		Update("balance", gorm.Expr("balance + ?", delta)).Error
}

func validateAmountSign(txType common.TransactionType, amount float64) error {
	if txType == common.TransactionTypeIncome && amount <= 0 {
		return common.NewBadRequest("Amount must be positive for income transactions")
	}
	if txType == common.TransactionTypeExpense && amount >= 0 {
		return common.NewBadRequest("Amount must be negative for expense transactions")
	}
	return nil
}

func (s *Service) Create(dto CreateTransactionDto, userId string) (*Transaction, error) {
	if replay, err := common.CreateReplay(s.db, "transaction", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.FindOne(*dto.ID, userId)
	}
	var acc account.Account
	if err := s.db.Where("id = ? AND \"userId\" = ?", dto.AccountId, userId).First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound("Account not found")
		}
		return nil, err
	}

	var cat category.Category
	if err := s.db.First(&cat, "id = ?", dto.CategoryId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound("Category not found")
		}
		return nil, err
	}

	if !cat.IsDefault && (cat.UserId == nil || *cat.UserId != userId) {
		return nil, common.NewBadRequest("Access denied to this category")
	}

	if cat.Type != string(dto.Type) {
		return nil, common.NewBadRequest("Category type must match transaction type")
	}

	if err := validateAmountSign(dto.Type, dto.Amount); err != nil {
		return nil, err
	}

	txDate := time.Now()
	if dto.TransactionDate != nil {
		txDate = *dto.TransactionDate
	}

	tx := &Transaction{
		Type:             string(dto.Type),
		Amount:           dto.Amount,
		Description:      dto.Description,
		CategoryId:       dto.CategoryId,
		UserId:           userId,
		AccountId:        dto.AccountId,
		TransactionDate:  txDate,
		OriginalCurrency: dto.OriginalCurrency,
		OriginalAmount:   dto.OriginalAmount,
		BalanceApplied:   true,
	}
	tx.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		tx.ID = *dto.ID
	}

	if err := s.db.Transaction(func(db *gorm.DB) error {
		if err := db.Create(tx).Error; err != nil {
			return err
		}
		return adjustBalance(db, tx.AccountId, tx.Amount)
	}); err != nil {
		return nil, err
	}
	acc.Balance += tx.Amount
	tx.Account = &acc
	return tx, nil
}

func (s *Service) FindAll(userId string, period common.TimePeriod, offset int) ([]Transaction, error) {
	fromDate := getDateRange(period, offset)
	var txs []Transaction
	err := s.withRelations(s.db).
		Where("\"userId\" = ? AND \"transactionDate\" >= ?", userId, fromDate).
		Order("\"transactionDate\" DESC").
		Find(&txs).Error
	return txs, err
}

func (s *Service) FindOne(id, userId string) (*Transaction, error) {
	var tx Transaction
	err := s.withRelations(s.db).
		Where("id = ? AND \"userId\" = ?", id, userId).
		First(&tx).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewNotFound("Transaction not found")
	}
	return &tx, err
}

func (s *Service) Update(id string, dto UpdateTransactionDto, userId string) (*Transaction, error) {
	tx, err := s.FindOne(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "transaction", id, userId, err)
	}
	if common.IsStale(tx.ClientModifiedAt, dto.ClientModifiedAt) {
		return tx, nil
	}

	if dto.AccountId != nil {
		var acc account.Account
		if err := s.db.Where("id = ? AND \"userId\" = ?", *dto.AccountId, userId).First(&acc).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, common.NewNotFound("Account not found")
			}
			return nil, err
		}
	}

	finalType := common.TransactionType(tx.Type)
	if dto.Type != nil {
		finalType = *dto.Type
	}

	finalCategoryType := ""
	if tx.Category != nil {
		finalCategoryType = tx.Category.Type
	}
	if dto.CategoryId != nil {
		var cat category.Category
		if err := s.db.First(&cat, "id = ?", *dto.CategoryId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, common.NewNotFound("Category not found")
			}
			return nil, err
		}
		if !cat.IsDefault && (cat.UserId == nil || *cat.UserId != userId) {
			return nil, common.NewBadRequest("Access denied to this category")
		}
		finalCategoryType = cat.Type
	}
	if finalCategoryType != "" && finalCategoryType != string(finalType) {
		return nil, common.NewBadRequest("Category type must match transaction type")
	}
	finalAmount := tx.Amount
	if dto.Amount != nil {
		finalAmount = *dto.Amount
	}

	if err := validateAmountSign(finalType, finalAmount); err != nil {
		return nil, err
	}

	oldAccountId, oldAmount := tx.AccountId, tx.Amount

	if dto.Type != nil {
		tx.Type = string(*dto.Type)
	}
	if dto.Amount != nil {
		tx.Amount = *dto.Amount
	}
	if dto.Description != nil {
		tx.Description = *dto.Description
	}
	if dto.CategoryId != nil {
		tx.CategoryId = *dto.CategoryId
	}
	if dto.AccountId != nil {
		tx.AccountId = *dto.AccountId
	}
	if dto.TransactionDate != nil {
		tx.TransactionDate = *dto.TransactionDate
	}
	if dto.OriginalCurrency != nil {
		tx.OriginalCurrency = dto.OriginalCurrency
	}
	if dto.OriginalAmount != nil {
		tx.OriginalAmount = dto.OriginalAmount
	}
	if dto.ClientModifiedAt != nil {
		tx.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}

	if err := s.db.Transaction(func(db *gorm.DB) error {
		// Omit associations: the preloaded Account/Category are stale when the IDs
		// changed, and GORM would otherwise copy their IDs back onto the foreign keys.
		if err := db.Omit(clause.Associations).Save(tx).Error; err != nil {
			return err
		}
		if !tx.BalanceApplied {
			return nil
		}
		// Reverse the old effect, then apply the new one.
		if err := adjustBalance(db, oldAccountId, -oldAmount); err != nil {
			return err
		}
		return adjustBalance(db, tx.AccountId, tx.Amount)
	}); err != nil {
		return nil, err
	}
	return s.FindOne(id, userId)
}

// Remove soft-deletes a transaction. For a transfer, the linked transaction on
// the other account is deleted with it.
func (s *Service) Remove(id, userId string) error {
	tx, err := s.FindOne(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "transaction", id, userId, err)
	}
	return s.db.Transaction(func(db *gorm.DB) error {
		if tx.RelatedTransactionId != nil {
			var related Transaction
			err := db.Where("id = ? AND \"userId\" = ?", *tx.RelatedTransactionId, userId).First(&related).Error
			if err == nil {
				if err := db.Delete(&related).Error; err != nil {
					return err
				}
				if related.BalanceApplied {
					if err := adjustBalance(db, related.AccountId, -related.Amount); err != nil {
						return err
					}
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if err := db.Delete(tx).Error; err != nil {
			return err
		}
		if !tx.BalanceApplied {
			return nil
		}
		return adjustBalance(db, tx.AccountId, -tx.Amount)
	})
}

func (s *Service) FindByAccount(accountId, userId string, period common.TimePeriod, offset int) ([]Transaction, error) {
	var acc account.Account
	if err := s.db.Where("id = ? AND \"userId\" = ?", accountId, userId).First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound("Account not found")
		}
		return nil, err
	}

	fromDate := getDateRange(period, offset)
	var txs []Transaction
	err := s.withRelations(s.db).
		Where("\"accountId\" = ? AND \"userId\" = ? AND \"transactionDate\" >= ?", accountId, userId, fromDate).
		Order("\"transactionDate\" DESC").
		Find(&txs).Error
	return txs, err
}

// transactionExists reports whether any row, of any user and including
// soft-deleted ones, has this id.
func transactionExists(db *gorm.DB, id string) (bool, error) {
	var n int64
	err := db.Raw(`SELECT COUNT(*) FROM "transaction" WHERE "id" = ?`, id).Scan(&n).Error
	return n > 0, err
}

func (s *Service) Transfer(dto CreateTransferDto, userId string) (*TransferResult, error) {
	if (dto.ID == nil) != (dto.RelatedID == nil) {
		return nil, common.NewBadRequest("id and relatedId must be sent together")
	}
	if dto.ID != nil && *dto.ID == *dto.RelatedID {
		return nil, common.NewBadRequest("id and relatedId must differ")
	}
	if replay, err := common.CreateReplay(s.db, "transaction", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		src, err := s.FindOne(*dto.ID, userId)
		if err != nil {
			return nil, err
		}
		if src.RelatedTransactionId == nil || *src.RelatedTransactionId != *dto.RelatedID {
			return nil, common.NewConflict("id belongs to a different transaction")
		}
		dst, err := s.FindOne(*dto.RelatedID, userId)
		if err != nil {
			return nil, err
		}
		return &TransferResult{SourceTransaction: src, DestinationTransaction: dst}, nil
	} else if dto.RelatedID != nil {
		// A new transfer must not adopt an existing row as its destination leg.
		inUse, err := transactionExists(s.db, *dto.RelatedID)
		if err != nil {
			return nil, err
		}
		if inUse {
			return nil, common.NewConflict("relatedId is already in use")
		}
	}
	if dto.SourceAccountId == dto.DestinationAccountId {
		return nil, common.NewBadRequest("Source and destination accounts cannot be the same")
	}

	var srcAcc account.Account
	if err := s.db.Where("id = ? AND \"userId\" = ?", dto.SourceAccountId, userId).First(&srcAcc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound("Source account not found")
		}
		return nil, err
	}

	var dstAcc account.Account
	if err := s.db.Where("id = ? AND \"userId\" = ?", dto.DestinationAccountId, userId).First(&dstAcc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound("Destination account not found")
		}
		return nil, err
	}

	var outCat category.Category
	if err := s.db.Where("name = ? AND \"isDefault\" = ? AND type = ?", transferOutCategory, true, "expense").First(&outCat).Error; err != nil {
		return nil, common.NewNotFound("Outgoing transfer category not found")
	}

	var inCat category.Category
	if err := s.db.Where("name = ? AND \"isDefault\" = ? AND type = ?", transferInCategory, true, "income").First(&inCat).Error; err != nil {
		return nil, common.NewNotFound("Incoming transfer category not found")
	}

	abs := math.Abs(dto.Amount)
	destAbs := abs
	if srcAcc.Currency != dstAcc.Currency {
		if dto.DestinationAmount == nil {
			return nil, common.NewBadRequest("destinationAmount is required for cross-currency transfers")
		}
		destAbs = math.Abs(*dto.DestinationAmount)
	} else if dto.DestinationAmount != nil {
		destAbs = math.Abs(*dto.DestinationAmount)
	}

	srcDto := CreateTransactionDto{
		CreateMeta:      common.CreateMeta{ID: dto.ID, ClientModifiedAt: dto.ClientModifiedAt},
		Type:            common.TransactionTypeExpense,
		Amount:          -abs,
		Description:     dto.Description,
		CategoryId:      outCat.ID,
		AccountId:       dto.SourceAccountId,
		TransactionDate: dto.TransactionDate,
	}
	dstDto := CreateTransactionDto{
		CreateMeta:      common.CreateMeta{ID: dto.RelatedID, ClientModifiedAt: dto.ClientModifiedAt},
		Type:            common.TransactionTypeIncome,
		Amount:          destAbs,
		Description:     dto.Description,
		CategoryId:      inCat.ID,
		AccountId:       dto.DestinationAccountId,
		TransactionDate: dto.TransactionDate,
	}

	var result *TransferResult
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		txSvc := &Service{db: tx}
		src, err := txSvc.Create(srcDto, userId)
		if err != nil {
			return err
		}
		dst, err := txSvc.Create(dstDto, userId)
		if err != nil {
			return err
		}
		// Link both sides so reports/budgets exclude them and deletes cascade.
		if err := tx.Model(&Transaction{}).Where("id = ?", src.ID).
			Update("relatedTransactionId", dst.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&Transaction{}).Where("id = ?", dst.ID).
			Update("relatedTransactionId", src.ID).Error; err != nil {
			return err
		}
		src.RelatedTransactionId = &dst.ID
		dst.RelatedTransactionId = &src.ID
		result = &TransferResult{SourceTransaction: src, DestinationTransaction: dst}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// maxValidateRowErrors caps the number of row-level errors returned by ValidateCsv.
// Unknown account/category scanning always runs to completion regardless of this limit.
const maxValidateRowErrors = 50

// csvContext holds the in-memory state used by ImportFromCsv (which needs the full
// dataset for duplicate frequency counting and the DB transaction).
type csvContext struct {
	colIdx    map[string]int
	rows      [][]string
	accByName map[string]*account.Account
	catByName map[string]*category.Category
}

// loadUserEntities fetches the user's accounts and categories from the DB and applies
// categoryMapping so that CSV category names resolve to existing category records.
func (s *Service) loadUserEntities(userId string, categoryMapping map[string]string) (map[string]*account.Account, map[string]*category.Category, error) {
	var userAccounts []account.Account
	if err := s.db.Where(`"userId" = ?`, userId).Find(&userAccounts).Error; err != nil {
		return nil, nil, err
	}
	var userCategories []category.Category
	if err := s.db.Where(`"userId" = ? OR "isDefault" = ?`, userId, true).Find(&userCategories).Error; err != nil {
		return nil, nil, err
	}

	accByName := make(map[string]*account.Account, len(userAccounts))
	for i := range userAccounts {
		accByName[userAccounts[i].Name] = &userAccounts[i]
	}
	catByName := make(map[string]*category.Category, len(userCategories))
	catById := make(map[string]*category.Category, len(userCategories))
	for i := range userCategories {
		catByName[userCategories[i].Name] = &userCategories[i]
		catById[userCategories[i].ID] = &userCategories[i]
	}

	// Mapping targets must be the user's own or default categories (catById);
	// anything else is ignored and surfaces as an unknown category.
	if len(categoryMapping) > 0 {
		for csvName, catId := range categoryMapping {
			if _, ok := catByName[csvName]; !ok {
				if cat, ok := catById[catId]; ok {
					catByName[csvName] = cat
				}
			}
		}
	}

	return accByName, catByName, nil
}

// parseCsvHeaders parses the first line of a CSV reader and returns the column index map.
func parseCsvHeaders(reader *csv.Reader) (map[string]int, error) {
	header, err := reader.Read()
	if err != nil {
		return nil, common.NewBadRequest(fmt.Sprintf("CSV parsing error: %s", err.Error()))
	}
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}
	required := []string{"Date", "Category", "Amount", "Currency", "Wallet"}
	var missing []string
	for _, col := range required {
		if _, ok := colIdx[col]; !ok {
			missing = append(missing, col)
		}
	}
	if len(missing) > 0 {
		return nil, common.NewBadRequest(fmt.Sprintf("Missing required columns: %s", strings.Join(missing, ", ")))
	}
	return colIdx, nil
}

// parseCsvContext loads all rows into memory (needed by ImportFromCsv for duplicate
// detection) and resolves user entities via loadUserEntities.
func (s *Service) parseCsvContext(csvData []byte, userId string, categoryMapping map[string]string) (*csvContext, error) {
	reader := csv.NewReader(bytes.NewReader(csvData))
	colIdx, err := parseCsvHeaders(reader)
	if err != nil {
		return nil, err
	}

	records, err := reader.ReadAll()
	if err != nil {
		return nil, common.NewBadRequest(fmt.Sprintf("CSV parsing error: %s", err.Error()))
	}
	if len(records) == 0 {
		return nil, common.NewBadRequest("CSV file is empty")
	}

	accByName, catByName, err := s.loadUserEntities(userId, categoryMapping)
	if err != nil {
		return nil, err
	}

	return &csvContext{colIdx: colIdx, rows: records, accByName: accByName, catByName: catByName}, nil
}

// validateRows checks amount, currency, and income/expense sign for rows in ctx.
// Rows referencing unknown accounts or categories are skipped.
func validateRows(ctx *csvContext) []string {
	var errs []string
	for i, r := range ctx.rows {
		rowNum := i + 2
		acc, accKnown := ctx.accByName[r[ctx.colIdx["Wallet"]]]
		cat, catKnown := ctx.catByName[r[ctx.colIdx["Category"]]]
		if !accKnown || !catKnown {
			continue
		}
		if _, err := parseTxDate(r[ctx.colIdx["Date"]]); err != nil {
			errs = append(errs, fmt.Sprintf("Row %d: invalid date %q", rowNum, r[ctx.colIdx["Date"]]))
			continue
		}
		amount, err := parseAmount(r[ctx.colIdx["Amount"]])
		if err != nil {
			errs = append(errs, fmt.Sprintf("Row %d: invalid amount %q", rowNum, r[ctx.colIdx["Amount"]]))
			continue
		}
		if acc.Currency != r[ctx.colIdx["Currency"]] {
			errs = append(errs, fmt.Sprintf("Row %d: currency mismatch — account uses %s, transaction uses %s", rowNum, acc.Currency, r[ctx.colIdx["Currency"]]))
			continue
		}
		if cat.Type == string(common.TransactionTypeIncome) && amount < 0 {
			errs = append(errs, fmt.Sprintf("Row %d: amount should be positive for income transactions", rowNum))
			continue
		}
		if cat.Type == string(common.TransactionTypeExpense) && amount > 0 {
			errs = append(errs, fmt.Sprintf("Row %d: amount should be negative for expense transactions", rowNum))
			continue
		}
	}
	return errs
}

// ValidateCsv streams the CSV row-by-row so peak memory is O(1) per row regardless
// of file size. A single pass collects unknown accounts, unknown categories, and
// row-level errors simultaneously. Row errors are capped at maxValidateRowErrors to
// bound response size; unknown name scanning always runs to completion.
func (s *Service) ValidateCsv(csvData []byte, userId string, categoryMapping map[string]string) (*ValidateResult, error) {
	accByName, catByName, err := s.loadUserEntities(userId, categoryMapping)
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(bytes.NewReader(csvData))
	reader.ReuseRecord = true // reuse the row slice to avoid per-row allocations
	colIdx, err := parseCsvHeaders(reader)
	if err != nil {
		return nil, err
	}

	unknownAccSet := make(map[string]struct{})
	unknownCatSet := make(map[string]struct{})
	var rowErrors []string
	rowNum := 1

	for {
		r, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, common.NewBadRequest(fmt.Sprintf("CSV parsing error at row %d: %s", rowNum+1, err.Error()))
		}
		rowNum++

		walletName := r[colIdx["Wallet"]]
		catName := r[colIdx["Category"]]
		acc, accKnown := accByName[walletName]
		cat, catKnown := catByName[catName]

		if !accKnown {
			unknownAccSet[walletName] = struct{}{}
		}
		if !catKnown {
			unknownCatSet[catName] = struct{}{}
		}

		// Skip row validation if entities are missing or the error cap is reached.
		if !accKnown || !catKnown || len(rowErrors) >= maxValidateRowErrors {
			continue
		}

		if _, err := parseTxDate(r[colIdx["Date"]]); err != nil {
			rowErrors = append(rowErrors, fmt.Sprintf("Row %d: invalid date %q", rowNum, r[colIdx["Date"]]))
			continue
		}
		amount, err := parseAmount(r[colIdx["Amount"]])
		if err != nil {
			rowErrors = append(rowErrors, fmt.Sprintf("Row %d: invalid amount %q", rowNum, r[colIdx["Amount"]]))
			continue
		}
		if acc.Currency != r[colIdx["Currency"]] {
			rowErrors = append(rowErrors, fmt.Sprintf("Row %d: currency mismatch — account uses %s, transaction uses %s", rowNum, acc.Currency, r[colIdx["Currency"]]))
			continue
		}
		if cat.Type == string(common.TransactionTypeIncome) && amount < 0 {
			rowErrors = append(rowErrors, fmt.Sprintf("Row %d: amount should be positive for income transactions", rowNum))
			continue
		}
		if cat.Type == string(common.TransactionTypeExpense) && amount > 0 {
			rowErrors = append(rowErrors, fmt.Sprintf("Row %d: amount should be negative for expense transactions", rowNum))
			continue
		}
	}

	return &ValidateResult{
		Valid:              len(unknownAccSet) == 0 && len(unknownCatSet) == 0 && len(rowErrors) == 0,
		UnknownAccounts:   sortedKeys(unknownAccSet),
		UnknownCategories: sortedKeys(unknownCatSet),
		RowErrors:         rowErrors,
	}, nil
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Service) ImportFromCsv(csvData []byte, userId string, categoryMapping map[string]string) (*ImportResult, error) {
	ctx, err := s.parseCsvContext(csvData, userId, categoryMapping)
	if err != nil {
		return nil, err
	}

	// Collect unique names used in the CSV.
	walletNames := make(map[string]struct{})
	catNames := make(map[string]struct{})
	for _, r := range ctx.rows {
		walletNames[r[ctx.colIdx["Wallet"]]] = struct{}{}
		catNames[r[ctx.colIdx["Category"]]] = struct{}{}
	}

	// Unknown accounts → hard error; unknown categories → return for FE to map.
	var accErrors []string
	for w := range walletNames {
		if _, ok := ctx.accByName[w]; !ok {
			accErrors = append(accErrors, fmt.Sprintf(`account "%s" not found`, w))
		}
	}
	if len(accErrors) > 0 {
		sort.Strings(accErrors)
		return nil, common.NewBadRequest(strings.Join(accErrors, ". ") + " Please create these first.")
	}

	var unknownCategories []string
	for c := range catNames {
		if _, ok := ctx.catByName[c]; !ok {
			unknownCategories = append(unknownCategories, c)
		}
	}
	if len(unknownCategories) > 0 {
		sort.Strings(unknownCategories)
		return &ImportResult{UnknownCategories: unknownCategories}, nil
	}

	rowErrors := validateRows(ctx)
	if len(rowErrors) > 0 {
		return &ImportResult{Errors: rowErrors}, nil
	}

	// Build parsed rows and CSV-frequency map for duplicate detection.
	type dupKey struct {
		date        string
		accountId   string
		categoryId  string
		amountCents int64
	}
	toDupKey := func(date time.Time, accountId, categoryId string, amount float64) dupKey {
		return dupKey{
			date:        date.UTC().Format("2006-01-02"),
			accountId:   accountId,
			categoryId:  categoryId,
			amountCents: int64(math.Round(amount * 100)),
		}
	}

	type parsedRow struct {
		acc    *account.Account
		cat    *category.Category
		amount float64
		date   time.Time
		note   string
		key    dupKey
	}

	parsed := make([]parsedRow, 0, len(ctx.rows))
	csvFreq := make(map[dupKey]int)
	var minDate, maxDate time.Time

	for _, r := range ctx.rows {
		acc := ctx.accByName[r[ctx.colIdx["Wallet"]]]
		cat := ctx.catByName[r[ctx.colIdx["Category"]]]
		amount, _ := parseAmount(r[ctx.colIdx["Amount"]])
		date, _ := parseTxDate(r[ctx.colIdx["Date"]]) // validated in validateRows

		note := ""
		if idx, ok := ctx.colIdx["Note"]; ok && idx < len(r) {
			note = r[idx]
		}
		if note == "" {
			note = "Imported transaction"
		}

		k := toDupKey(date, acc.ID, cat.ID, amount)
		csvFreq[k]++
		if minDate.IsZero() || date.Before(minDate) {
			minDate = date
		}
		if date.After(maxDate) {
			maxDate = date
		}
		parsed = append(parsed, parsedRow{acc: acc, cat: cat, amount: amount, date: date, note: note, key: k})
	}

	// Query existing transactions in date range for duplicate detection.
	// quota = csvCount[key] - dbCount[key] so two genuinely identical transactions
	// on the same day are each imported exactly once.
	dbFreq := make(map[dupKey]int)
	if !minDate.IsZero() {
		var existing []Transaction
		endDate := maxDate.AddDate(0, 0, 1)
		if err := s.db.
			Where(`"userId" = ? AND "transactionDate" >= ? AND "transactionDate" < ?`, userId, minDate, endDate).
			Select(`"accountId", "categoryId", amount, "transactionDate"`).
			Find(&existing).Error; err != nil {
			return nil, err
		}
		for _, tx := range existing {
			k := toDupKey(tx.TransactionDate, tx.AccountId, tx.CategoryId, tx.Amount)
			dbFreq[k]++
		}
	}

	var imported, skipped int
	importedPerKey := make(map[dupKey]int)
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		txSvc := &Service{db: tx}
		for _, p := range parsed {
			quota := csvFreq[p.key] - dbFreq[p.key]
			if importedPerKey[p.key] >= quota {
				skipped++
				continue
			}
			dto := CreateTransactionDto{
				Type:            common.TransactionType(p.cat.Type),
				Amount:          p.amount,
				Description:     p.note,
				CategoryId:      p.cat.ID,
				AccountId:       p.acc.ID,
				TransactionDate: &p.date,
			}
			if _, err := txSvc.Create(dto, userId); err != nil {
				return err
			}
			importedPerKey[p.key]++
			imported++
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &ImportResult{Imported: imported, Skipped: skipped}, nil
}

func parseTxDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func parseAmount(s string) (float64, error) {
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == '.' || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return strconv.ParseFloat(b.String(), 64)
}
