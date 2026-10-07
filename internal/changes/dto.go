package changes

import (
	"ger_fin_api/internal/account"
	"ger_fin_api/internal/budget"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/recurring_transaction"
	"ger_fin_api/internal/transaction"
	"ger_fin_api/internal/user"
)

// ChangeSet holds live rows changed since the cursor, as raw model rows
// (relations as ids, no computed fields).
type ChangeSet struct {
	Accounts              []account.Account                            `json:"accounts"`
	Categories            []category.Category                          `json:"categories"`
	Transactions          []transaction.Transaction                    `json:"transactions"`
	Budgets               []budget.Budget                              `json:"budgets"`
	RecurringTransactions []recurring_transaction.RecurringTransaction `json:"recurringTransactions"`
	UserSettings          []user.UserSettings                          `json:"userSettings"`
}

// DeletedRef identifies a row soft-deleted since the cursor.
type DeletedRef struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
}

// Response is one page of the change feed.
type Response struct {
	Changes ChangeSet    `json:"changes"`
	Deleted []DeletedRef `json:"deleted"`
	Cursor  int64        `json:"cursor"`
	HasMore bool         `json:"hasMore"`
}

func newResponse(cursor int64) *Response {
	return &Response{
		Changes: ChangeSet{
			Accounts:              []account.Account{},
			Categories:            []category.Category{},
			Transactions:          []transaction.Transaction{},
			Budgets:               []budget.Budget{},
			RecurringTransactions: []recurring_transaction.RecurringTransaction{},
			UserSettings:          []user.UserSettings{},
		},
		Deleted: []DeletedRef{},
		Cursor:  cursor,
	}
}
