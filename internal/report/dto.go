package report

type CategoryRow struct {
	CategoryId   string  `json:"categoryId"`
	CategoryName string  `json:"categoryName"`
	Total        float64 `json:"total"`
	Percentage   float64 `json:"percentage"`
}

type CategoryBreakdownResponse struct {
	BaseCurrency string        `json:"baseCurrency"`
	Income       []CategoryRow `json:"income"`
	Expense      []CategoryRow `json:"expense"`
}

type MonthRow struct {
	Month   string  `json:"month"`
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
	Net     float64 `json:"net"`
}

type MonthlyTrendResponse struct {
	BaseCurrency string     `json:"baseCurrency"`
	Months       []MonthRow `json:"months"`
}

type NetWorthAccount struct {
	AccountId           string   `json:"accountId"`
	Name                string   `json:"name"`
	Currency            string   `json:"currency"`
	Balance             float64  `json:"balance"`
	BaseCurrencyBalance *float64 `json:"baseCurrencyBalance,omitempty"`
}

type NetWorthResponse struct {
	Total        float64           `json:"total"`
	BaseCurrency string            `json:"baseCurrency"`
	Accounts     []NetWorthAccount `json:"accounts"`
}

type CashFlowPeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type CashFlowSummary struct {
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
	Net     float64 `json:"net"`
}

type CashFlowRecurring struct {
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
}

type CashFlowResponse struct {
	Period    CashFlowPeriod    `json:"period"`
	Actual    CashFlowSummary   `json:"actual"`
	Projected CashFlowSummary   `json:"projected"`
	Recurring CashFlowRecurring `json:"recurring"`
}

type YearRow struct {
	Year int        `json:"year"`
	Data []MonthRow `json:"data"`
}

type YearOverYearResponse struct {
	Months []string  `json:"months"`
	Years  []YearRow `json:"years"`
}
