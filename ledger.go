package main

import (
	"encoding/json"
	"time"

	"github.com/erc7824/go-nitrolite"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Entry represents a ledger entry in the database
type Entry struct {
	ID          uint            `gorm:"primaryKey"`
	AccountID   common.Hash     `gorm:"column:account_id;not null;type:binary(32);index:idx_account_asset;index:idx_account_participant"`
	AccountType AccountType     `gorm:"column:account_type;not null"`
	Participant string          `gorm:"column:participant;not null;index:idx_account_participant"`
	AssetID     string          `gorm:"column:asset_id;not null;index:idx_account_asset"`
	Credit      decimal.Decimal `gorm:"column:credit;type:decimal(38,18);not null"`
	Debit       decimal.Decimal `gorm:"column:debit;type:decimal(38,18);not null"`
	CreatedAt   time.Time
}

func (Entry) TableName() string {
	return "ledger"
}

// ChannelAccount tracks on-chain balance locked in a channel.
func GetChannelAccountID(channel nitrolite.Channel) common.Hash {
	return nitrolite.GetChannelID(channel)
}

// LedgerAccount tracks unified balance abstracted across chains.
func GetLedgerAccountID(participantAddress common.Address) common.Hash {
	return crypto.Keccak256Hash(participantAddress.Bytes())
}

// AppSessionAccount tracks funds in a virtual app session.
func GetAppSessionAccountID(app AppDefinition) common.Hash {
	b, _ := json.Marshal(app)
	return crypto.Keccak256Hash(b)
}

type Ledger struct {
	participant string
	db          *gorm.DB
}

func GetLedger(db *gorm.DB, participant string) *Ledger {
	return &Ledger{participant: participant, db: db}
}

func (l *Ledger) Record(accountID common.Hash, assetID string, amount decimal.Decimal) error {
	entry := &Entry{
		AccountID:   accountID,
		Participant: l.participant,
		AssetID:     assetID,
		Credit:      decimal.Zero,
		Debit:       decimal.Zero,
		CreatedAt:   time.Now(),
	}

	if amount.IsPositive() {
		entry.Credit = amount
	} else if amount.IsNegative() {
		entry.Debit = amount.Abs()
	} else {
		return nil
	}

	return l.db.Create(entry).Error
}

func (l *Ledger) Balance(accountID common.Hash, assetID string) (decimal.Decimal, error) {
	type result struct {
		Balance decimal.Decimal `gorm:"column:balance"`
	}
	var res result
	if err := l.db.Model(&Entry{}).
		Where("account_id = ? AND asset_id = ?", accountID, assetID).
		Select("COALESCE(SUM(credit),0) - COALESCE(SUM(debit),0) AS balance").
		Scan(&res).Error; err != nil {
		return decimal.Zero, err
	}
	return res.Balance, nil
}

type Balance struct {
	Asset  string          `json:"asset"`
	Amount decimal.Decimal `json:"amount"`
}

func (l *Ledger) GetBalances(accountID common.Hash) ([]Balance, error) {
	type row struct {
		Asset   string          `gorm:"column:asset_id"`
		Balance decimal.Decimal `gorm:"column:balance"`
	}

	var rows []row
	if err := l.db.
		Model(&Entry{}).
		Where("account_id = ? AND participant = ?", accountID, l.participant).
		Select("asset_id", "COALESCE(SUM(credit),0) - COALESCE(SUM(debit),0) AS balance").
		Group("asset_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	balances := make([]Balance, len(rows))
	for i, r := range rows {
		balances[i] = Balance{
			Asset:  r.Asset,
			Amount: r.Balance,
		}
	}
	return balances, nil
}
