package currency

type RateResponse struct {
	BaseCurrency   string  `json:"baseCurrency"`
	TargetCurrency string  `json:"targetCurrency"`
	Rate           float64 `json:"rate"`
	FetchedAt      string  `json:"fetchedAt"`
}

type RefreshResponse struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}
