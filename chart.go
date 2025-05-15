package main

// AccountType represents the type of account in the ledger system
type AccountType uint16

const (
	// Assets (1000-1999)
	AssetDefault AccountType = 1000

	// Liabilities (2000-2999)
	LiabilityDefault AccountType = 2000

	// Equity/Capital (3000-3999)
	EquityDefault AccountType = 3000

	// Revenue (4000-4999)
	RevenueDefault AccountType = 4000

	// Expenses (5000-5999)
	ExpenseDefault AccountType = 5000
)
