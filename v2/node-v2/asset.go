package main

import (
	"errors"
	"fmt"
)

type Asset struct {
	ID string // 'usdc', 'weth'
}

var asset_mapping = map[string]Asset{
	"137_usdc_polygon_adress": Asset{ID: "usdc"},
	"1_usdc_eth_adress":       Asset{ID: "usdc"},
}

func mapTokenToAsset(tokenAddress string, networkID uint64) (Asset, error) {
	identifier := fmt.Sprint("%v_%s", networkID, tokenAddress)

	asset, ok := asset_mapping[identifier]
	if !ok {
		return Asset{}, errors.New("unknown token")
	}

	return asset, nil
}
