package main

import (
	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

type Asset struct {
	ID       uint           `gorm:"primaryKey;column:id;autoIncrement"`
	AssetID  string         `gorm:"column:asset_id;index"` // "usdc" - internal unified asset id
	Token    common.Address `gorm:"column:token;not null;uniqueIndex:token_chain"`
	ChainID  uint32         `gorm:"column:chain_id;not null;uniqueIndex:token_chain"`
	Symbol   string         `gorm:"column:symbol;not null"`
	Decimals uint8          `gorm:"column:decimals;not null"`
}

func (Asset) TableName() string {
	return "assets"
}

func GetAssetByToken(db *gorm.DB, token string, chainID uint32) (*Asset, error) {
	var asset Asset
	err := db.Where("token = ? AND chain_id = ?", token, chainID).First(&asset).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &asset, err
}

func GetTokenByAsset(db *gorm.DB, assetID string, chainID uint32) (*Asset, error) {
	var asset Asset
	err := db.Where("asset_id = ? AND chain_id = ?", assetID, chainID).First(&asset).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &asset, err
}
