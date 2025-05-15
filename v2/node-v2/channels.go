package main

import "github.com/shopspring/decimal"

// Represent on-chain channel
type Channel struct {
	ParticipantAddress string // participant address
	NetworkID          uint64
	TokenAddress       string // token address in the network
	OnChainBalance     decimal.Decimal
}

func (c Channel) GetAssociatedLedgerAccountID() string {
	asset, err := mapTokenToAsset(c.TokenAddress, c.NetworkID)
	if err != nil {
		panic(err)
	}

	return getLedgerAccountID("", c.ParticipantAddress, asset)
}

type ChannelRepo struct {
	channels []*Channel
}

func NewChannelRepo() *ChannelRepo {
	return &ChannelRepo{
		channels: make([]*Channel, 0),
	}
}

func (c *ChannelRepo) findChannelsAssociatedWithAccount(accountID string) []Channel {
	associatedChannels := make([]Channel, 0)
	for _, ch := range c.channels {
		if accountID == ch.GetAssociatedLedgerAccountID() {
			associatedChannels = append(associatedChannels, *ch)
		}
	}

	return associatedChannels
}
