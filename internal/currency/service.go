package currency

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"gorm.io/gorm"
)

var ErrRateNotFound = errors.New("exchange rate not found")

// ErrRefreshTooSoon is returned when a refresh is requested within minRefreshInterval
// of the previous one; it protects the OXR API quota from being exhausted by clients.
var ErrRefreshTooSoon = errors.New("exchange rates were refreshed recently")

const minRefreshInterval = 10 * time.Minute

type Service struct {
	db    *gorm.DB
	appID string

	refreshMu   sync.Mutex
	lastRefresh time.Time
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
	resp, err := client.Get(fmt.Sprintf("https://openexchangerates.org/api/latest.json?app_id=%s", url.QueryEscape(s.appID)))
	if err != nil {
		// *url.Error embeds the request URL, which contains the app_id; strip it before logging.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
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

	return s.db.Transaction(func(tx *gorm.DB) error {
		// Load existing live rows for this base currency
		var existing []ExchangeRate
		if err := tx.Where(`"baseCurrency" = ?`, payload.Base).Find(&existing).Error; err != nil {
			return fmt.Errorf("load existing rates: %w", err)
		}
		existingMap := make(map[string]*ExchangeRate, len(existing))
		for i := range existing {
			existingMap[existing[i].TargetCurrency] = &existing[i]
		}

		now := time.Now()
		var toCreate []ExchangeRate
		for target, rate := range payload.Rates {
			if er, ok := existingMap[target]; ok {
				er.Rate = rate
				er.FetchedAt = now
				if err := tx.Save(er).Error; err != nil {
					return fmt.Errorf("update rate %s/%s: %w", payload.Base, target, err)
				}
			} else {
				toCreate = append(toCreate, ExchangeRate{
					BaseCurrency:   payload.Base,
					TargetCurrency: target,
					Rate:           rate,
					FetchedAt:      now,
				})
			}
		}
		if len(toCreate) > 0 {
			if err := tx.Create(&toCreate).Error; err != nil {
				return fmt.Errorf("insert new rates: %w", err)
			}
		}

		log.Printf("currency: refreshed %d exchange rates (base: %s, updated: %d, new: %d)",
			len(payload.Rates), payload.Base, len(existing), len(toCreate))
		return nil
	})
}

// RefreshRates is the user-triggered refresh: like FetchAndStoreRates but
// rate-limited across all users to one call per minRefreshInterval.
func (s *Service) RefreshRates() error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if !s.lastRefresh.IsZero() && time.Since(s.lastRefresh) < minRefreshInterval {
		return ErrRefreshTooSoon
	}
	if err := s.FetchAndStoreRates(); err != nil {
		return err
	}
	s.lastRefresh = time.Now()
	return nil
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
