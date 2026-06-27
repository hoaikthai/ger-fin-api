package currency

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"
)

var ErrRateNotFound = errors.New("exchange rate not found")

type Service struct {
	db    *gorm.DB
	appID string
}

func NewService(db *gorm.DB, appID string) *Service {
	return &Service{db: db, appID: appID}
}

func (s *Service) FetchAndStoreRates() error {
	if s.appID == "" {
		log.Println("currency: OXR_APP_ID not set, skipping rate fetch")
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://openexchangerates.org/api/latest.json?app_id=%s", s.appID))
	if err != nil {
		return fmt.Errorf("fetch rates from OXR: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OXR API returned status %d", resp.StatusCode)
	}

	var payload struct {
		Base  string             `json:"base"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode OXR response: %w", err)
	}

	now := time.Now()
	rates := make([]ExchangeRate, 0, len(payload.Rates))
	for target, rate := range payload.Rates {
		rates = append(rates, ExchangeRate{
			BaseCurrency:   payload.Base,
			TargetCurrency: target,
			Rate:           rate,
			FetchedAt:      now,
		})
	}

	// Soft-delete all existing rates for this base so the unique index stays clean,
	// then bulk-insert new rates — wrapped in a transaction so a failed create
	// never leaves the table empty.
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(`"baseCurrency" = ?`, payload.Base).Delete(&ExchangeRate{}).Error; err != nil {
			return fmt.Errorf("clear old rates: %w", err)
		}
		if err := tx.Create(&rates).Error; err != nil {
			return fmt.Errorf("store rates: %w", err)
		}
		log.Printf("currency: refreshed %d exchange rates (base: %s)", len(rates), payload.Base)
		return nil
	})
}

func (s *Service) Convert(amount float64, from, to string) (float64, error) {
	if from == to {
		return amount, nil
	}

	var needed []string
	if from != "USD" {
		needed = append(needed, from)
	}
	if to != "USD" {
		needed = append(needed, to)
	}

	rateMap := make(map[string]float64)
	if len(needed) > 0 {
		var rows []ExchangeRate
		if err := s.db.
			Where(`"baseCurrency" = ? AND "targetCurrency" IN ?`, "USD", needed).
			Order(`"fetchedAt" DESC`).
			Find(&rows).Error; err != nil {
			return 0, err
		}
		for _, r := range rows {
			if _, exists := rateMap[r.TargetCurrency]; !exists {
				rateMap[r.TargetCurrency] = r.Rate
			}
		}
	}

	for _, c := range needed {
		if _, ok := rateMap[c]; !ok {
			return 0, fmt.Errorf("%w: USD/%s", ErrRateNotFound, c)
		}
	}

	// Convert: from → USD → to
	usdAmount := amount
	if from != "USD" {
		usdAmount = amount / rateMap[from]
	}
	if to == "USD" {
		return usdAmount, nil
	}
	return usdAmount * rateMap[to], nil
}

func (s *Service) GetLatestRates() ([]ExchangeRate, error) {
	var rates []ExchangeRate
	err := s.db.Order(`"targetCurrency" ASC`).Find(&rates).Error
	return rates, err
}
