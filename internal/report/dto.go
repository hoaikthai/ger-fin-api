package report

type CategoryRow struct {
	CategoryId   string  `json:"categoryId"`
	CategoryName string  `json:"categoryName"`
	Total        float64 `json:"total"`
	Percentage   float64 `json:"percentage"`
}

type CategoryBreakdownResponse struct {
	Income  []CategoryRow `json:"income"`
	Expense []CategoryRow `json:"expense"`
}

type MonthRow struct {
	Month   string  `json:"month"`
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
	Net     float64 `json:"net"`
}

type NetWorthAccount struct {
	AccountId string  `json:"accountId"`
	Name      string  `json:"name"`
	Currency  string  `json:"currency"`
	Balance   float64 `json:"balance"`
}

type NetWorthResponse struct {
	Total    float64           `json:"total"`
	Accounts []NetWorthAccount `json:"accounts"`
}
