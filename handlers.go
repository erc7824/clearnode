package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/erc7824/go-nitrolite"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// AppDefinition represents the definition of an application on the ledger
type AppDefinition struct {
	Protocol     string   `json:"protocol"`
	Participants []string `json:"participants"` // Participants from channels with broker.
	Weights      []uint64 `json:"weights"`      // Signature weight for each participant.
	Quorum       uint64   `json:"quorum"`
	Challenge    uint64   `json:"challenge"`
	Nonce        uint64   `json:"nonce,omitempty"`
}

// CreateAppSessionParams represents parameters needed for virtual app creation
type CreateAppSessionParams struct {
	Definition  AppDefinition   `json:"definition"`
	Allocations []AppAllocation `json:"allocations"`
}

type AppAllocation struct {
	Participant string          `json:"participant"`
	AssetSymbol string          `json:"asset_symbol"`
	Amount      decimal.Decimal `json:"amount"`
}

type CreateAppSignData struct {
	RequestID uint64
	Method    string
	Params    []CreateAppSessionParams
	Timestamp uint64
}

func (r CreateAppSignData) MarshalJSON() ([]byte, error) {
	arr := []interface{}{r.RequestID, r.Method, r.Params, r.Timestamp}
	return json.Marshal(arr)
}

// CloseAppSessionParams represents parameters needed for virtual app closure
type CloseAppSessionParams struct {
	AppSessionID string          `json:"app_session_id"`
	Allocations  []AppAllocation `json:"allocations"`
}

type CloseAppSignData struct {
	RequestID uint64
	Method    string
	Params    []CloseAppSessionParams
	Timestamp uint64
}

func (r CloseAppSignData) MarshalJSON() ([]byte, error) {
	arr := []interface{}{r.RequestID, r.Method, r.Params, r.Timestamp}
	return json.Marshal(arr)
}

// AppSessionResponse represents response data for application operations
type AppSessionResponse struct {
	AppSessionID string `json:"app_session_id"`
	Status       string `json:"status"`
}

// ResizeChannelParams represents parameters needed for resizing a channel
type ResizeChannelParams struct {
	ChannelID         string          `json:"channel_id"`
	ParticipantChange decimal.Decimal `json:"participant_change"` // how much user wants to deposit or withdraw.
	FundsDestination  string          `json:"funds_destination"`
}

// ResizeChannelResponse represents the response for resizing a channel
type ResizeChannelResponse struct {
	ChannelID   string       `json:"channel_id"`
	StateData   string       `json:"state_data"`
	Intent      uint8        `json:"intent"`
	Version     *big.Int     `json:"version"`
	Allocations []Allocation `json:"allocations"`
	StateHash   string       `json:"state_hash"`
	Signature   Signature    `json:"server_signature"`
}

// Allocation represents a token allocation for a specific participant
type Allocation struct {
	Participant  string   `json:"destination"`
	TokenAddress string   `json:"token"`
	Amount       *big.Int `json:"amount,string"`
}

type ResizeChannelSignData struct {
	RequestID uint64
	Method    string
	Params    []ResizeChannelParams
	Timestamp uint64
}

func (r ResizeChannelSignData) MarshalJSON() ([]byte, error) {
	arr := []interface{}{r.RequestID, r.Method, r.Params, r.Timestamp}
	return json.Marshal(arr)
}

// CloseChannelParams represents parameters needed for channel closure
type CloseChannelParams struct {
	ChannelID        string `json:"channel_id"`
	FundsDestination string `json:"funds_destination"`
}

// CloseChannelResponse represents the response for closing a channel
type CloseChannelResponse struct {
	ChannelID        string       `json:"channel_id"`
	Intent           uint8        `json:"intent"`
	Version          *big.Int     `json:"version"`
	StateData        string       `json:"state_data"`
	FinalAllocations []Allocation `json:"allocations"`
	StateHash        string       `json:"state_hash"`
	Signature        Signature    `json:"server_signature"`
}

// ChannelResponse represents a channel's details in the response
type ChannelResponse struct {
	ChannelID   string        `json:"channel_id"`
	Participant string        `json:"participant"`
	Status      ChannelStatus `json:"status"`
	Token       string        `json:"token"`
	// Total amount in the channel (user + broker)
	Amount    uint64 `json:"amount"`
	ChainID   uint32 `json:"network_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Signature struct {
	V uint8  `json:"v,string"`
	R string `json:"r,string"`
	S string `json:"s,string"`
}

// BrokerConfig represents the broker configuration information
type BrokerConfig struct {
	BrokerAddress string `json:"brokerAddress"`
}

// RPCEntry represents an RPC record from history.
type RPCEntry struct {
	ID        uint     `json:"id"`
	Sender    string   `json:"sender"`
	ReqID     uint64   `json:"req_id"`
	Method    string   `json:"method"`
	Params    string   `json:"params"`
	Timestamp uint64   `json:"timestamp"`
	ReqSig    []string `json:"req_sig"`
	Result    string   `json:"response"`
	ResSig    []string `json:"res_sig"`
}

// HandleGetConfig returns the broker configuration
func HandleGetConfig(rpc *RPCRequest) (*RPCResponse, error) {
	config := BrokerConfig{
		BrokerAddress: BrokerAddress,
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, "get_config", []any{config}, time.Now())
	return rpcResponse, nil
}

// HandlePing responds to a ping request with a pong response in RPC format
func HandlePing(rpc *RPCRequest) (*RPCResponse, error) {
	return CreateResponse(rpc.Req.RequestID, "pong", []any{}, time.Now()), nil
}

// HandleGetLedgerBalances returns a list of participants and their balances for a ledger account
func HandleGetLedgerBalances(rpc *RPCRequest, address string, db *gorm.DB) (*RPCResponse, error) {
	var accountID string

	if len(rpc.Req.Params) > 0 {
		paramsJSON, err := json.Marshal(rpc.Req.Params[0])
		if err == nil {
			var params map[string]string
			if err := json.Unmarshal(paramsJSON, &params); err == nil {
				accountID = params["acc"]
			}
		}
	}

	ledger := GetParticipantLedger(db, address)
	balances, err := ledger.GetBalances(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to find account: %w", err)
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{balances}, time.Now())
	return rpcResponse, nil
}

// HandleCreateApplication creates a virtual application between participants
func HandleCreateApplication(rpc *RPCRequest, db *gorm.DB) (*RPCResponse, error) {
	if len(rpc.Req.Params) < 1 {
		return nil, errors.New("missing parameters")
	}

	var createApp CreateAppSessionParams
	paramsJSON, err := json.Marshal(rpc.Req.Params[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if err := json.Unmarshal(paramsJSON, &createApp); err != nil {
		return nil, fmt.Errorf("invalid parameters format: %w", err)
	}

	if len(createApp.Definition.Participants) < 2 {
		return nil, errors.New("invalid number of participants")
	}

	// Allocation should be specified for each participant even if it is zero.
	if len(createApp.Allocations) != len(createApp.Definition.Participants) {
		return nil, errors.New("number of allocations must be equal to participants")
	}

	if len(createApp.Definition.Weights) != len(createApp.Definition.Participants) {
		return nil, errors.New("number of weights must be equal to participants")
	}

	var participantsAddresses []common.Address
	for _, participant := range createApp.Definition.Participants {
		participantsAddresses = append(participantsAddresses, common.HexToAddress(participant))
	}

	if createApp.Definition.Nonce == 0 {
		createApp.Definition.Nonce = rpc.Req.Timestamp
	}

	// Generate a unique ID for the virtual application
	b, _ := json.Marshal(createApp.Definition)
	appSessionID := crypto.Keccak256Hash(b)

	req := CreateAppSignData{
		RequestID: rpc.Req.RequestID,
		Method:    rpc.Req.Method,
		Params:    []CreateAppSessionParams{{Definition: createApp.Definition, Allocations: createApp.Allocations}},
		Timestamp: rpc.Req.Timestamp,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, errors.New("error serializing message")
	}

	recoveredAddresses := map[string]bool{}
	for _, sig := range rpc.Sig {
		addr, err := RecoverAddress(reqBytes, sig)
		if err != nil {
			return nil, errors.New("invalid signature")
		}
		recoveredAddresses[addr] = true
	}

	// Use a transaction to ensure atomicity for the entire operation
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, allocation := range createApp.Allocations {
			if allocation.Amount.IsNegative() {
				return errors.New("invalid allocation")
			}
			if allocation.Amount.IsPositive() {
				if !recoveredAddresses[allocation.Participant] {
					return fmt.Errorf("missing signature for participant %s", allocation.Participant)
				}
			}

			participantLedger := GetParticipantLedger(tx, allocation.Participant)
			balance, err := participantLedger.Balance(allocation.Participant, allocation.AssetSymbol)
			if err != nil {
				return fmt.Errorf("failed to check participant balance: %w", err)
			}
			if allocation.Amount.GreaterThan(balance) {
				return errors.New("insufficient funds")
			}
			if err := participantLedger.Record(allocation.Participant, allocation.AssetSymbol, allocation.Amount.Neg()); err != nil {
				return fmt.Errorf("failed to transfer funds from participant: %w", err)
			}
			if err := participantLedger.Record(appSessionID.Hex(), allocation.AssetSymbol, allocation.Amount); err != nil {
				return fmt.Errorf("failed to transfer funds to virtual app: %w", err)
			}
		}

		weights := pq.Int64Array{}
		for _, v := range createApp.Definition.Weights {
			weights = append(weights, int64(v))
		}

		// Record the virtual app creation in state
		appSession := &AppSession{
			Protocol:     createApp.Definition.Protocol,
			SessionID:    appSessionID.Hex(),
			Participants: createApp.Definition.Participants,
			Status:       ChannelStatusOpen,
			Challenge:    createApp.Definition.Challenge,
			Weights:      weights,
			Quorum:       createApp.Definition.Quorum,
			Nonce:        createApp.Definition.Nonce,
			Version:      rpc.Req.Timestamp,
		}

		if err := tx.Create(appSession).Error; err != nil {
			return fmt.Errorf("failed to record virtual app: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	response := &AppSessionResponse{
		AppSessionID: appSessionID.Hex(),
		Status:       string(ChannelStatusOpen),
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{response}, time.Now())
	return rpcResponse, nil
}

// HandleCloseApplication closes a virtual app session and redistributes funds to participants
func HandleCloseApplication(rpc *RPCRequest, db *gorm.DB) (*RPCResponse, error) {
	if len(rpc.Req.Params) == 0 {
		return nil, errors.New("missing parameters")
	}

	var params CloseAppSessionParams
	paramsJSON, err := json.Marshal(rpc.Req.Params[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters format: %w", err)
	}

	if params.AppSessionID == "" || len(params.Allocations) == 0 {
		return nil, errors.New("missing required parameters: app_id or allocations")
	}

	assets := map[string]struct{}{}
	for _, a := range params.Allocations {
		if a.Participant == "" || a.AssetSymbol == "" || a.Amount.IsNegative() {
			return nil, errors.New("invalid allocation row")
		}
		assets[a.AssetSymbol] = struct{}{}
	}

	req := CloseAppSignData{
		RequestID: rpc.Req.RequestID,
		Method:    rpc.Req.Method,
		Params:    []CloseAppSessionParams{{AppSessionID: params.AppSessionID, Allocations: params.Allocations}},
		Timestamp: rpc.Req.Timestamp,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, errors.New("error serializing message")
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		var appSession AppSession
		if err := tx.Where("session_id = ? AND status = ?", params.AppSessionID, ChannelStatusOpen).Order("nonce DESC").
			First(&appSession).Error; err != nil {
			return fmt.Errorf("virtual app not found or not open: %w", err)
		}

		participantWeights := map[string]int64{}
		for i, addr := range appSession.Participants {
			participantWeights[strings.ToLower(addr)] = appSession.Weights[i]
		}

		seen := map[string]bool{}
		var totalWeight int64
		for _, sigHex := range rpc.Sig {
			recovered, err := RecoverAddress(reqBytes, sigHex)
			if err != nil {
				return err
			}
			recovered = strings.ToLower(recovered)
			if seen[recovered] {
				return errors.New("duplicate signature")
			}
			seen[recovered] = true
			weight, ok := participantWeights[recovered]
			if !ok {
				return fmt.Errorf("signature from unknown participant %s", recovered)
			}
			if weight <= 0 {
				return fmt.Errorf("zero weight for signer %s", recovered)
			}
			totalWeight += weight
		}
		if totalWeight < int64(appSession.Quorum) {
			return fmt.Errorf("quorum not met: %d / %d", totalWeight, appSession.Quorum)
		}

		sessionBal := map[string]decimal.Decimal{}

		for _, p := range appSession.Participants {
			ledger := GetParticipantLedger(tx, p)
			for asset := range assets {
				bal, err := ledger.Balance(appSession.SessionID, asset)
				if err != nil {
					return fmt.Errorf("failed to read balance for %s:%s: %w", p, asset, err)
				}
				sessionBal[asset] = sessionBal[asset].Add(bal)
			}
		}

		allocationSum := map[string]decimal.Decimal{}
		participantsSeen := map[string]bool{}

		for _, alloc := range params.Allocations {
			addr := strings.ToLower(alloc.Participant)
			if _, ok := participantWeights[addr]; !ok {
				return fmt.Errorf("allocation to non-participant %s", alloc.Participant)
			}
			if participantsSeen[addr] {
				return fmt.Errorf("participant %s appears more than once", alloc.Participant)
			}
			participantsSeen[addr] = true

			ledger := GetParticipantLedger(tx, alloc.Participant)
			balance, err := ledger.Balance(appSession.SessionID, alloc.AssetSymbol)
			if err != nil {
				return fmt.Errorf("failed to get participant balance: %w", err)
			}
			if !balance.Equal(alloc.Amount) {
				return fmt.Errorf("allocation mismatch for %s in %s: expected %s, got %s",
					alloc.Participant, alloc.AssetSymbol, balance, alloc.Amount)
			}

			// Debit session, credit participant
			if err := ledger.Record(appSession.SessionID, alloc.AssetSymbol, balance.Neg()); err != nil {
				return fmt.Errorf("failed to debit session: %w", err)
			}
			if err := ledger.Record(alloc.Participant, alloc.AssetSymbol, alloc.Amount); err != nil {
				return fmt.Errorf("failed to credit participant: %w", err)
			}

			allocationSum[alloc.AssetSymbol] = allocationSum[alloc.AssetSymbol].Add(alloc.Amount)
		}

		// Every participant must appear exactly once
		if len(participantsSeen) != len(appSession.Participants) {
			return errors.New("allocations must be provided for every participant exactly once")
		}

		for asset, bal := range sessionBal {
			if alloc, ok := allocationSum[asset]; !ok || !bal.Equal(alloc) {
				return fmt.Errorf("asset %s not fully redistributed", asset)
			}
		}
		for asset := range allocationSum {
			if _, ok := sessionBal[asset]; !ok {
				return fmt.Errorf("allocation references unknown asset %s", asset)
			}
		}

		return tx.Model(&appSession).Updates(map[string]any{
			"status":     ChannelStatusClosed,
			"updated_at": time.Now(),
		}).Error
	})

	if err != nil {
		return nil, err
	}

	response := &AppSessionResponse{
		AppSessionID: params.AppSessionID,
		Status:       string(ChannelStatusClosed),
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{response}, time.Now())
	return rpcResponse, nil
}

// HandleGetAppDefinition returns the application definition for a ledger account
func HandleGetAppDefinition(rpc *RPCRequest, db *gorm.DB) (*RPCResponse, error) {
	var accountID string

	if len(rpc.Req.Params) > 0 {
		paramsJSON, err := json.Marshal(rpc.Req.Params[0])
		if err == nil {
			var params map[string]string
			if err := json.Unmarshal(paramsJSON, &params); err == nil {
				accountID = params["acc"]
			}
		}
	}

	if accountID == "" {
		return nil, errors.New("missing account ID")
	}

	var vApp AppSession
	if err := db.Where("session_id = ?", accountID).First(&vApp).Error; err != nil {
		return nil, fmt.Errorf("failed to find application: %w", err)
	}

	appDef := AppDefinition{
		Protocol:     vApp.Protocol,
		Participants: vApp.Participants,
		Weights:      make([]uint64, len(vApp.Participants)), // Default weights to 0 for now
		Quorum:       vApp.Quorum,                            // Default quorum to 100 for now
		Challenge:    vApp.Challenge,
		Nonce:        vApp.Nonce,
	}

	for i := range vApp.Weights {
		appDef.Weights[i] = uint64(vApp.Weights[i])
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{appDef}, time.Now())
	return rpcResponse, nil
}

// HandleResizeChannel processes a request to resize a payment channel
func HandleResizeChannel(rpc *RPCRequest, db *gorm.DB, signer *Signer) (*RPCResponse, error) {
	if len(rpc.Req.Params) < 1 {
		return nil, errors.New("missing parameters")
	}

	var params ResizeChannelParams
	paramsJSON, err := json.Marshal(rpc.Req.Params[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters format: %w", err)
	}

	channel, err := GetChannelByID(db, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("failed to find channel: %w", err)
	}

	req := ResizeChannelSignData{
		RequestID: rpc.Req.RequestID,
		Method:    rpc.Req.Method,
		Params:    []ResizeChannelParams{{ChannelID: params.ChannelID, ParticipantChange: params.ParticipantChange, FundsDestination: params.FundsDestination}},
		Timestamp: rpc.Req.Timestamp,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, errors.New("error serializing message")
	}

	isValid, err := ValidateSignature(reqBytes, rpc.Sig[0], channel.Participant)
	if err != nil || !isValid {
		return nil, errors.New("invalid signature")
	}

	asset, err := GetAssetBySymbol(db, channel.Token, channel.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to find asset: %w", err)
	}
	if asset == nil {
		return nil, fmt.Errorf("asset not found: %s", channel.Token)
	}

	ledger := GetParticipantLedger(db, channel.Participant)
	balance, err := ledger.Balance(channel.ChannelID, asset.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to check participant A balance: %w", err)
	}

	if balance.LessThan(params.ParticipantChange) {
		return nil, errors.New("insufficient unified balance")
	}

	rawNewChannelAmount := params.ParticipantChange.Shift(int32(asset.Decimals)).BigInt()
	brokerPart := channel.Amount - rawNewChannelAmount.Uint64()

	allocations := []nitrolite.Allocation{
		{
			Destination: common.HexToAddress(params.FundsDestination),
			Token:       common.HexToAddress(channel.Token),
			Amount:      rawNewChannelAmount,
		},
		{
			Destination: common.HexToAddress(BrokerAddress),
			Token:       common.HexToAddress(channel.Token),
			Amount:      big.NewInt(0),
		},
	}

	resizeAmounts := []*big.Int{big.NewInt(0), big.NewInt(-int64(brokerPart))} // Always release broker funds if there is a surplus.

	intentionType, err := abi.NewType("int256[]", "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create ABI type for intentions: %w", err)
	}

	intentionsArgs := abi.Arguments{
		{Type: intentionType},
	}

	encodedIntentions, err := intentionsArgs.Pack(resizeAmounts)
	if err != nil {
		return nil, fmt.Errorf("failed to pack intentions: %w", err)
	}

	// Encode the channel ID and state for signing
	channelID := common.HexToHash(channel.ChannelID)
	encodedState, err := nitrolite.EncodeState(channelID, nitrolite.IntentRESIZE, big.NewInt(int64(channel.Version)+1), encodedIntentions, allocations)
	if err != nil {
		return nil, fmt.Errorf("failed to encode state hash: %w", err)
	}

	// Generate state hash and sign it
	stateHash := crypto.Keccak256Hash(encodedState).Hex()
	sig, err := signer.NitroSign(encodedState)
	if err != nil {
		return nil, fmt.Errorf("failed to sign state: %w", err)
	}

	response := ResizeChannelResponse{
		ChannelID: channel.ChannelID,
		Intent:    uint8(nitrolite.IntentRESIZE),
		Version:   big.NewInt(int64(channel.Version) + 1),
		StateData: hexutil.Encode(encodedIntentions),
		StateHash: stateHash,
		Signature: Signature{
			V: sig.V,
			R: hexutil.Encode(sig.R[:]),
			S: hexutil.Encode(sig.S[:]),
		},
	}

	for _, alloc := range allocations {
		response.Allocations = append(response.Allocations, Allocation{
			Participant:  alloc.Destination.Hex(),
			TokenAddress: alloc.Token.Hex(),
			Amount:       alloc.Amount,
		})
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{response}, time.Now())
	return rpcResponse, nil
}

// HandleCloseChannel processes a request to close a payment channel
func HandleCloseChannel(rpc *RPCRequest, db *gorm.DB, signer *Signer) (*RPCResponse, error) {
	if len(rpc.Req.Params) < 1 {
		return nil, errors.New("missing parameters")
	}

	var params CloseChannelParams
	paramsJSON, err := json.Marshal(rpc.Req.Params[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters format: %w", err)
	}

	channel, err := GetChannelByID(db, params.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("failed to find channel: %w", err)
	}

	reqBytes, err := json.Marshal(rpc.Req)
	if err != nil {
		return nil, errors.New("error serializing message")
	}

	isValid, err := ValidateSignature(reqBytes, rpc.Sig[0], channel.Participant)
	if err != nil || !isValid {
		return nil, errors.New("invalid signature")
	}

	asset, err := GetAssetBySymbol(db, channel.Token, channel.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to find asset: %w", err)
	}
	if asset == nil {
		return nil, fmt.Errorf("asset not found: %s", channel.Token)
	}

	ledger := GetParticipantLedger(db, channel.Participant)
	balance, err := ledger.Balance(channel.ChannelID, asset.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to check participant A balance: %w", err)
	}

	if balance.IsNegative() {
		return nil, errors.New("insufficient funds for participant: " + channel.Token)
	}

	rawBalance := balance.Shift(int32(asset.Decimals)).BigInt()

	channelAmount := new(big.Int).SetUint64(channel.Amount)
	if channelAmount.Cmp(rawBalance) < 0 {
		return nil, errors.New("resize this channel first")
	}

	allocations := []nitrolite.Allocation{
		{
			Destination: common.HexToAddress(params.FundsDestination),
			Token:       common.HexToAddress(channel.Token),
			Amount:      rawBalance,
		},
		{
			Destination: common.HexToAddress(BrokerAddress),
			Token:       common.HexToAddress(channel.Token),
			Amount:      new(big.Int).Sub(channelAmount, rawBalance), // Broker receives the remaining amount
		},
	}

	stateDataStr := "0x"
	stateData, err := hexutil.Decode(stateDataStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode state data: %w", err)
	}

	channelID := common.HexToHash(channel.ChannelID)
	encodedState, err := nitrolite.EncodeState(channelID, nitrolite.IntentFINALIZE, big.NewInt(int64(channel.Version)+1), stateData, allocations)
	if err != nil {
		return nil, fmt.Errorf("failed to encode state hash: %w", err)
	}

	stateHash := crypto.Keccak256Hash(encodedState).Hex()
	sig, err := signer.NitroSign(encodedState)
	if err != nil {
		return nil, fmt.Errorf("failed to sign state: %w", err)
	}

	response := CloseChannelResponse{
		ChannelID: channel.ChannelID,
		Intent:    uint8(nitrolite.IntentFINALIZE),
		Version:   big.NewInt(int64(channel.Version) + 1),
		StateData: stateDataStr,
		StateHash: stateHash,
		Signature: Signature{
			V: sig.V,
			R: hexutil.Encode(sig.R[:]),
			S: hexutil.Encode(sig.S[:]),
		},
	}

	for _, alloc := range allocations {
		response.FinalAllocations = append(response.FinalAllocations, Allocation{
			Participant:  alloc.Destination.Hex(),
			TokenAddress: alloc.Token.Hex(),
			Amount:       alloc.Amount,
		})
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{response}, time.Now())
	return rpcResponse, nil
}

// HandleGetChannels returns a list of channels for a given account
// TODO: add filters, pagination, etc.
func HandleGetChannels(rpc *RPCRequest, db *gorm.DB) (*RPCResponse, error) {
	var participant string

	if len(rpc.Req.Params) > 0 {
		paramsJSON, err := json.Marshal(rpc.Req.Params[0])
		if err == nil {
			var params map[string]string
			if err := json.Unmarshal(paramsJSON, &params); err == nil {
				participant = params["participant"]
			}
		}
	}

	if participant == "" {
		return nil, errors.New("missing participant parameter")
	}

	reqBytes, err := json.Marshal(rpc.Req)
	if err != nil {
		return nil, errors.New("error serializing message")
	}

	isValid, err := ValidateSignature(reqBytes, rpc.Sig[0], participant)
	if err != nil || !isValid {
		return nil, errors.New("invalid signature")
	}

	var channelResponses []ChannelResponse

	err = db.Transaction(func(tx *gorm.DB) error {
		channels, err := getChannelsForParticipant(tx, participant)
		if err != nil {
			return fmt.Errorf("failed to get channels: %w", err)
		}

		for _, channel := range channels {
			channelResponses = append(channelResponses, ChannelResponse{
				ChannelID:   channel.ChannelID,
				Participant: channel.Participant,
				Status:      channel.Status,
				Token:       channel.Token,
				Amount:      channel.Amount,
				ChainID:     channel.ChainID,
				CreatedAt:   channel.CreatedAt.Format(time.RFC3339),
				UpdatedAt:   channel.UpdatedAt.Format(time.RFC3339),
			})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{channelResponses}, time.Now())
	return rpcResponse, nil
}

func HandleGetRPCHistory(participant string, rpc *RPCRequest, store *RPCStore) (*RPCResponse, error) {
	if participant == "" {
		return nil, errors.New("missing participant parameter")
	}

	var rpcHistory []RPCRecord
	if err := store.db.Where("sender = ?", participant).Order("timestamp DESC").Find(&rpcHistory).Error; err != nil {
		return nil, fmt.Errorf("failed to retrieve RPC history: %w", err)
	}

	response := make([]RPCEntry, 0, len(rpcHistory))
	for _, record := range rpcHistory {
		response = append(response, RPCEntry{
			ID:        record.ID,
			Sender:    record.Sender,
			ReqID:     record.ReqID,
			Method:    record.Method,
			Params:    string(record.Params),
			Timestamp: record.Timestamp,
			ReqSig:    record.ReqSig,
			ResSig:    record.ResSig,
			Result:    string(record.Response),
		})
	}

	rpcResponse := CreateResponse(rpc.Req.RequestID, rpc.Req.Method, []any{response}, time.Now())
	return rpcResponse, nil
}
