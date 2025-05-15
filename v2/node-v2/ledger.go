package main

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// Participant represent the user of Clearnet
type Participant struct {
	Adress string
}

// LedgerAccount is unique asset account per user
// It means, that if user has deposited USDC on Polygon and CELO,
// both deposits will go to this account
//
// # Why is has to be unique
//
// If this account is unique, we can open vapps without specifying user ledger accounts.
// It potentially solves the challenges we faced in current version
//
// Another opportunity we get with LedgerAccounts separated from channels, is that user can recive payments for assets,
// he doesn't have associated channels with. In order to withdraw such assets, user may provision the channel later.
// He also can use this asset to pay, without ever having associated channel
//
// Also user may have as many associated channels as he wants. User is able to withdraw the asset though any assciated channel
//
// # VappSessionID and ParticipantAddress
//
// User ledger account has only ParticipantAddress specified.
// When VAppSession is initiated, we provision new ledger accounts for each participant of the vApp, with both VappSessionId and ParticipantAddress specified.
// This way we can always tell user total balance, and user available balance
//
// User available balance = balance in LedgerAccount{VappSessionId: "", ParticipantAddress: "userAddress"}
//
// User total balance = SUM(user main ledger account + all user ledger accounts associated with vApps)
//
// Also, when we have new state in vApp, we may reflect the changes immediately in ledger
type LedgerAccount struct {
	VappSessionId      string // is this account associated with any vAppSession
	ParticipantAddress string // to which participant this account belongs
	Asset              Asset  // which asset(chain-agnostic) this account represents
	Balance            decimal.Decimal
}

func getLedgerAccountID(vAppSessionId string, partipantAddress string, asset Asset) string {
	return fmt.Sprintf("%s_%s_%s", vAppSessionId, partipantAddress, asset.ID)
}

func (l LedgerAccount) ID() string {
	return getLedgerAccountID(l.VappSessionId, l.ParticipantAddress, l.Asset)
}

func (l LedgerAccount) Credit(amount decimal.Decimal) {
	l.Balance.Sub(amount)
}

func (l LedgerAccount) Debit(amount decimal.Decimal) {
	l.Balance.Add(amount)
}

// Ledger is a table which tracks the movement of funds
type Ledger struct {
	accounts map[string]*LedgerAccount
}

func NewLedger() *Ledger {
	return &Ledger{
		accounts: make(map[string]*LedgerAccount),
	}
}

type ParticipantBalance struct {
	balances map[string]*AssetBalance
}

type AssetBalance struct {
	Total              decimal.Decimal
	Available          decimal.Decimal
	AssociatedChannels []Channel
}

func (l *Ledger) createLedgerAccount(participantAddress string, vAppSessionId string, asset Asset) (*LedgerAccount, error) {
	acc := &LedgerAccount{
		VappSessionId:      vAppSessionId,
		ParticipantAddress: participantAddress,
		Asset:              asset,
		Balance:            decimal.Zero,
	}

	accID := acc.ID()
	if _, ok := l.accounts[accID]; ok {
		return nil, errors.New("account already exists")
	}

	l.accounts[accID] = acc

	return acc, nil

}

func (l *Ledger) GetLedgerAccount(participantAddress string, vAppSessionId string, asset Asset) *LedgerAccount {
	accID := getLedgerAccountID(vAppSessionId, participantAddress, asset)

	acc, ok := l.accounts[accID]
	// we implicitly provisioning the ledger account if needed
	if !ok {
		acc, _ = l.createLedgerAccount(participantAddress, vAppSessionId, asset)
	}

	return acc

}

func (l *Ledger) GetParticipantBalance(participantAddress string) ParticipantBalance {
	var assetBalances = make(map[string]*AssetBalance)

	for _, acc := range l.accounts {
		if acc.ParticipantAddress != participantAddress {
			continue
		}

		assetBalance, ok := assetBalances[acc.Asset.ID]
		if !ok {
			assetBalance = &AssetBalance{
				Total:     decimal.Zero,
				Available: decimal.Zero,
			}

			assetBalances[acc.Asset.ID] = assetBalance
		}

		assetBalance.Total.Add(acc.Balance)

		if acc.VappSessionId == "" {
			assetBalance.Available.Add(acc.Balance)
		}
	}

	for assetID, _ := range assetBalances {
		accID := getLedgerAccountID("", participantAddress, Asset{ID: assetID})
		associatedChannels := channelRepo.findChannelsAssociatedWithAccount(accID)
		assetBalances[assetID].AssociatedChannels = associatedChannels
	}

	return ParticipantBalance{balances: assetBalances}
}
