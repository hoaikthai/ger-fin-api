package transaction

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"

	"gorm.io/gorm"
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
		Type:            string(dto.Type),
		Amount:          dto.Amount,
		Description:     dto.Description,
		CategoryId:      dto.CategoryId,
		UserId:          userId,
		AccountId:       dto.AccountId,
		TransactionDate: txDate,
	}

	if err := s.db.Create(tx).Error; err != nil {
		return nil, err
	}
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
		return nil, err
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
	}

	finalType := common.TransactionType(tx.Type)
	if dto.Type != nil {
		finalType = *dto.Type
	}
	finalAmount := tx.Amount
	if dto.Amount != nil {
		finalAmount = *dto.Amount
	}

	if err := validateAmountSign(finalType, finalAmount); err != nil {
		return nil, err
	}

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

	if err := s.db.Save(tx).Error; err != nil {
		return nil, err
	}
	return tx, nil
}

func (s *Service) Remove(id, userId string) error {
	tx, err := s.FindOne(id, userId)
	if err != nil {
		return err
	}
	return s.db.Delete(tx).Error
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

func (s *Service) Transfer(dto CreateTransferDto, userId string) (*TransferResult, error) {
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
	srcDto := CreateTransactionDto{
		Type:            common.TransactionTypeExpense,
		Amount:          -abs,
		Description:     dto.Description,
		CategoryId:      outCat.ID,
		AccountId:       dto.SourceAccountId,
		TransactionDate: dto.TransactionDate,
	}
	dstDto := CreateTransactionDto{
		Type:            common.TransactionTypeIncome,
		Amount:          abs,
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
		result = &TransferResult{SourceTransaction: src, DestinationTransaction: dst}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ImportFromCsv(csvData []byte, userId string) (*ImportResult, error) {
	reader := csv.NewReader(bytes.NewReader(csvData))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, common.NewBadRequest(fmt.Sprintf("CSV parsing error: %s", err.Error()))
	}

	if len(records) < 2 {
		return nil, common.NewBadRequest("CSV file is empty")
	}

	// Build column index map from header row
	colIdx := make(map[string]int, len(records[0]))
	for i, h := range records[0] {
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

	rows := records[1:]

	// Load user accounts and categories once
	var userAccounts []account.Account
	if err := s.db.Where("\"userId\" = ?", userId).Find(&userAccounts).Error; err != nil {
		return nil, err
	}

	var userCategories []category.Category
	if err := s.db.Where("\"userId\" = ? OR \"isDefault\" = ?", userId, true).Find(&userCategories).Error; err != nil {
		return nil, err
	}

	accByName := make(map[string]*account.Account, len(userAccounts))
	for i := range userAccounts {
		accByName[userAccounts[i].Name] = &userAccounts[i]
	}
	catByName := make(map[string]*category.Category, len(userCategories))
	for i := range userCategories {
		catByName[userCategories[i].Name] = &userCategories[i]
	}

	// Pre-validate: all wallets and categories in the file must exist
	walletNames := make(map[string]struct{})
	catNames := make(map[string]struct{})
	for _, r := range rows {
		walletNames[r[colIdx["Wallet"]]] = struct{}{}
		catNames[r[colIdx["Category"]]] = struct{}{}
	}

	var preErrors []string
	for w := range walletNames {
		if _, ok := accByName[w]; !ok {
			preErrors = append(preErrors, fmt.Sprintf(`account "%s" not found`, w))
		}
	}
	for cat := range catNames {
		if _, ok := catByName[cat]; !ok {
			preErrors = append(preErrors, fmt.Sprintf(`category "%s" not found`, cat))
		}
	}
	if len(preErrors) > 0 {
		return nil, common.NewBadRequest(strings.Join(preErrors, ". ") + " Please create these first.")
	}

	// First pass: validate every row
	var rowErrors []string
	for i, r := range rows {
		rowNum := i + 2 // 1-based, +1 for header
		wallet := r[colIdx["Wallet"]]
		catName := r[colIdx["Category"]]
		currency := r[colIdx["Currency"]]
		amountStr := r[colIdx["Amount"]]

		acc := accByName[wallet]
		cat := catByName[catName]

		amount, err := parseAmount(amountStr)
		if err != nil {
			rowErrors = append(rowErrors, fmt.Sprintf("Row %d: invalid amount %q", rowNum, amountStr))
			continue
		}

		if acc.Currency != currency {
			rowErrors = append(rowErrors, fmt.Sprintf("Row %d: currency mismatch — account uses %s, transaction uses %s", rowNum, acc.Currency, currency))
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

	if len(rowErrors) > 0 {
		return &ImportResult{Imported: 0, Errors: rowErrors}, nil
	}

	// Second pass: import all rows inside a single transaction so a failure rolls back all inserts.
	var imported int
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		txSvc := &Service{db: tx}
		for _, r := range rows {
			wallet := r[colIdx["Wallet"]]
			catName := r[colIdx["Category"]]
			dateStr := r[colIdx["Date"]]
			amountStr := r[colIdx["Amount"]]

			acc := accByName[wallet]
			cat := catByName[catName]

			amount, _ := parseAmount(amountStr)
			txDate, _ := time.Parse("2006-01-02", dateStr)
			if txDate.IsZero() {
				txDate, _ = time.Parse(time.RFC3339, dateStr)
			}

			note := ""
			if idx, ok := colIdx["Note"]; ok && idx < len(r) {
				note = r[idx]
			}
			if note == "" {
				note = "Imported transaction"
			}

			dto := CreateTransactionDto{
				Type:            common.TransactionType(cat.Type),
				Amount:          amount,
				Description:     note,
				CategoryId:      cat.ID,
				AccountId:       acc.ID,
				TransactionDate: &txDate,
			}

			if _, err := txSvc.Create(dto, userId); err != nil {
				return err
			}
			imported++
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &ImportResult{Imported: imported, Errors: []string{}}, nil
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
