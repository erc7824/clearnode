package main

import (
	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

type Asset struct {
	Token    common.Address `gorm:"column:token;primaryKey"`    // part of primaryKey
	ChainID  uint32         `gorm:"column:chain_id;primaryKey"` // part of primaryKey
	Symbol   string         `gorm:"column:symbol;index"`        // e.g. "usdc"
	Decimals uint8          `gorm:"column:decimals;not null"`
}

func (Asset) TableName() string {
	return "assets"
}

func GetAssetByToken(db *gorm.DB, tokenAddress string, chainID uint32) (*Asset, error) {
	var asset Asset
	err := db.Where("token = ? AND chain_id = ?", tokenAddress, chainID).First(&asset).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &asset, err
}

func GetAssetBySymbol(db *gorm.DB, symbol string, chainID uint32) (*Asset, error) {
	var asset Asset
	err := db.Where("symbol = ? AND chain_id = ?", symbol, chainID).First(&asset).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &asset, err
}
