package common

type TransactionType string

const (
	TransactionTypeIncome  TransactionType = "income"
	TransactionTypeExpense TransactionType = "expense"
)

type RecurrenceFrequency string

const (
	RecurrenceFrequencyDaily   RecurrenceFrequency = "daily"
	RecurrenceFrequencyWeekly  RecurrenceFrequency = "weekly"
	RecurrenceFrequencyMonthly RecurrenceFrequency = "monthly"
	RecurrenceFrequencyYearly  RecurrenceFrequency = "yearly"
)

type TimePeriod string

const (
	TimePeriodDay     TimePeriod = "day"
	TimePeriodWeek    TimePeriod = "week"
	TimePeriodMonth   TimePeriod = "month"
	TimePeriodQuarter TimePeriod = "quarter"
	TimePeriodYear    TimePeriod = "year"
)
