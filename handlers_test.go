package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	container "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestSqlite creates an in-memory SQLite DB for testing
func setupTestSqlite(t testing.TB) *gorm.DB {
	t.Helper()

	// Generate a unique DSN for the in-memory DB to avoid sharing data between tests
	uniqueDSN := fmt.Sprintf("file::memory:test%s?mode=memory&cache=shared", uuid.NewString())

	db, err := gorm.Open(sqlite.Open(uniqueDSN), &gorm.Config{})
	require.NoError(t, err)

	// Auto migrate all required models
	err = db.AutoMigrate(&Entry{}, &Channel{}, &AppSession{}, &RPCRecord{})
	require.NoError(t, err)

	return db
}

// setupTestPostgres creates a PostgreSQL database using testcontainers
func setupTestPostgres(ctx context.Context, t testing.TB) (*gorm.DB, testcontainers.Container) {
	t.Helper()

	const dbName = "postgres"
	const dbUser = "postgres"
	const dbPassword = "postgres"

	// Start the PostgreSQL container
	postgresContainer, err := container.Run(ctx,
		"postgres:16-alpine",
		container.WithDatabase(dbName),
		container.WithUsername(dbUser),
		container.WithPassword(dbPassword),
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_HOST_AUTH_METHOD": "trust",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForLog("database system is ready to accept connections"),
				wait.ForListeningPort("5432/tcp"),
			)))
	require.NoError(t, err)
	log.Println("Started container:", postgresContainer.GetContainerID())

	// Get connection string
	url, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	log.Println("PostgreSQL URL:", url)

	// Connect to database
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{})
	require.NoError(t, err)

	// Auto migrate all required models
	err = db.AutoMigrate(&Entry{}, &Channel{}, &AppSession{}, &RPCRecord{})
	require.NoError(t, err)

	return db, postgresContainer
}

// setupTestDB creates a test database based on the TEST_DB_DRIVER environment variable
func setupTestDB(t testing.TB) (*gorm.DB, func()) {
	t.Helper()

	// Create a context with the test timeout
	ctx := context.Background()

	var db *gorm.DB
	var cleanup func()

	switch os.Getenv("TEST_DB_DRIVER") {
	case "postgres":
		log.Println("Using PostgreSQL for testing")
		var container testcontainers.Container
		db, container = setupTestPostgres(ctx, t)
		cleanup = func() {
			if container != nil {
				if err := container.Terminate(ctx); err != nil {
					log.Printf("Failed to terminate PostgreSQL container: %v", err)
				}
			}
		}
	default:
		log.Println("Using SQLite for testing (default)")
		db = setupTestSqlite(t)
		cleanup = func() {} // No cleanup needed for SQLite in-memory database
	}

	return db, cleanup
}

// TestHandlePing tests the ping handler functionality
func TestHandlePing(t *testing.T) {
	// Test case 1: Simple ping with no parameters
	rpcRequest1 := &RPCRequest{
		Req: RPCData{
			RequestID: 1,
			Method:    "ping",
			Params:    []any{nil},
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{"dummy-signature"},
	}

	response1, err := HandlePing(rpcRequest1)
	require.NoError(t, err)
	assert.NotNil(t, response1)

	require.Equal(t, "pong", response1.Res.Method)
}

// TestHandleCloseVirtualApp tests the close virtual app handler functionality
func TestHandleCloseVirtualApp(t *testing.T) {
	raw, err := crypto.GenerateKey()
	require.NoError(t, err)

	signer := Signer{privateKey: raw}
	participantA := signer.GetAddress().Hex()
	participantB := "0xParticipantB"

	db, cleanup := setupTestDB(t)
	defer cleanup()

	tokenAddress := "0xToken123"
	require.NoError(t, db.Create(&Channel{
		ChannelID:   "0xChannelA",
		Participant: participantA,
		Status:      ChannelStatusOpen,
		Token:       tokenAddress,
		Nonce:       1,
	}).Error)
	require.NoError(t, db.Create(&Channel{
		ChannelID:   "0xChannelB",
		Participant: participantB,
		Status:      ChannelStatusOpen,
		Token:       tokenAddress,
		Nonce:       1,
	}).Error)

	// Create a virtual app
	vAppID := "0xVApp123"
	require.NoError(t, db.Create(&AppSession{
		SessionID:    vAppID,
		Participants: []string{participantA, participantB},
		Status:       ChannelStatusOpen,
		Challenge:    60,
		Weights:      []int64{100, 0},
		Quorum:       100,
	}).Error)

	assetSymbol := "usdc"

	require.NoError(t, GetParticipantLedger(db, participantA).Record(vAppID, assetSymbol, decimal.NewFromInt(200)))
	require.NoError(t, GetParticipantLedger(db, participantB).Record(vAppID, assetSymbol, decimal.NewFromInt(300)))

	closeParams := CloseAppSessionParams{
		AppSessionID: vAppID,
		Allocations: []AppAllocation{
			{Participant: participantA, AssetSymbol: assetSymbol, Amount: decimal.NewFromInt(250)},
			{Participant: participantB, AssetSymbol: assetSymbol, Amount: decimal.NewFromInt(250)},
		},
	}

	// Create RPC request
	paramsJSON, _ := json.Marshal(closeParams)
	req := &RPCRequest{
		Req: RPCData{
			RequestID: 1,
			Method:    "close_app_session",
			Params:    []any{json.RawMessage(paramsJSON)},
			Timestamp: uint64(time.Now().Unix()),
		},
	}

	signData := CloseAppSignData{
		RequestID: req.Req.RequestID,
		Method:    req.Req.Method,
		Params:    []CloseAppSessionParams{closeParams},
		Timestamp: req.Req.Timestamp,
	}
	signBytes, _ := json.Marshal(signData)
	sig, _ := signer.Sign(signBytes)
	req.Sig = []string{hexutil.Encode(sig)}

	resp, err := HandleCloseApplication(req, db)
	require.NoError(t, err)
	assert.Equal(t, "close_app_session", resp.Res.Method)
	var updated AppSession
	require.NoError(t, db.Where("session_id = ?", vAppID).First(&updated).Error)
	assert.Equal(t, ChannelStatusClosed, updated.Status)

	// Check that funds were transferred back to channels according to allocations
	balA, _ := GetParticipantLedger(db, participantA).Balance(participantA, "usdc")
	balB, _ := GetParticipantLedger(db, participantB).Balance(participantB, "usdc")
	assert.Equal(t, decimal.NewFromInt(250), balA)
	assert.Equal(t, decimal.NewFromInt(250), balB)

	// ► v-app accounts drained
	vBalA, _ := GetParticipantLedger(db, participantA).Balance(vAppID, "usdc")
	vBalB, _ := GetParticipantLedger(db, participantB).Balance(vAppID, "usdc")

	assert.True(t, vBalA.IsZero(), "Participant A vApp balance should be zero")
	assert.True(t, vBalB.IsZero(), "Participant B vApp balance should be zero")
}

func TestHandleCreateVirtualApp(t *testing.T) {
	// Generate private keys for both participants
	rawA, _ := crypto.GenerateKey()
	rawB, _ := crypto.GenerateKey()
	signerA := Signer{privateKey: rawA}
	signerB := Signer{privateKey: rawB}
	addrA := signerA.GetAddress().Hex()
	addrB := signerB.GetAddress().Hex()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	// open direct channels (still required elsewhere in code-base)
	token := "0xTokenXYZ"
	for i, p := range []string{addrA, addrB} {
		ch := &Channel{
			ChannelID:   fmt.Sprintf("0xChannel%c", 'A'+i),
			Participant: p,
			Status:      ChannelStatusOpen,
			Token:       token,
			Nonce:       1,
		}
		require.NoError(t, db.Create(ch).Error)
	}

	require.NoError(t, GetParticipantLedger(db, addrA).Record(addrA, "usdc", decimal.NewFromInt(100)))
	require.NoError(t, GetParticipantLedger(db, addrB).Record(addrB, "usdc", decimal.NewFromInt(200)))

	ts := uint64(time.Now().Unix())
	def := AppDefinition{
		Protocol:     "test-proto",
		Participants: []string{addrA, addrB},
		Weights:      []uint64{1, 1},
		Quorum:       2,
		Challenge:    60,
		Nonce:        ts, // if omitted, handler would use ts anyway
	}
	asset := "usdc"
	createParams := CreateAppSessionParams{
		Definition: def,
		Allocations: []AppAllocation{
			{Participant: addrA, AssetSymbol: asset, Amount: decimal.NewFromInt(100)},
			{Participant: addrB, AssetSymbol: asset, Amount: decimal.NewFromInt(200)},
		},
	}

	rpcReq := &RPCRequest{
		Req: RPCData{
			RequestID: 42,
			Method:    "create_app_session",
			Params:    []any{createParams},
			Timestamp: ts,
		},
	}

	// sign exactly like the handler
	signData := CreateAppSignData{
		RequestID: rpcReq.Req.RequestID,
		Method:    rpcReq.Req.Method,
		Params:    []CreateAppSessionParams{createParams},
		Timestamp: rpcReq.Req.Timestamp,
	}
	signBytes, _ := signData.MarshalJSON()
	sigA, _ := signerA.Sign(signBytes)
	sigB, _ := signerB.Sign(signBytes)
	rpcReq.Sig = []string{hexutil.Encode(sigA), hexutil.Encode(sigB)}

	resp, err := HandleCreateApplication(rpcReq, db)
	require.NoError(t, err)

	// ► response sanity
	assert.Equal(t, "create_app_session", resp.Res.Method)
	appResp, ok := resp.Res.Params[0].(*AppSessionResponse)
	require.True(t, ok)
	assert.Equal(t, string(ChannelStatusOpen), appResp.Status)

	// ► v-app row exists
	var vApp AppSession
	require.NoError(t, db.Where("session_id = ?", appResp.AppSessionID).First(&vApp).Error)
	assert.ElementsMatch(t, []string{addrA, addrB}, vApp.Participants)

	// ► participant accounts drained
	partBalA, _ := GetParticipantLedger(db, addrA).Balance(addrA, "usdc")
	partBalB, _ := GetParticipantLedger(db, addrB).Balance(addrB, "usdc")
	assert.True(t, partBalA.IsZero(), "Participant A balance should be zero")
	assert.True(t, partBalB.IsZero(), "Participant B balance should be zero")

	// ► virtual-app funded - each participant can see the total app session balance (300)
	vBalA, _ := GetParticipantLedger(db, addrA).Balance(appResp.AppSessionID, "usdc")
	vBalB, _ := GetParticipantLedger(db, addrB).Balance(appResp.AppSessionID, "usdc")
	assert.Equal(t, decimal.NewFromInt(100).String(), vBalA.String())
	assert.Equal(t, decimal.NewFromInt(200).String(), vBalB.String())
}

// TestHandleListParticipants tests the list available channels handler functionality
func TestHandleListParticipants(t *testing.T) {
	// Set up test database with cleanup
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ledger := GetParticipantLedger(db, "0xParticipant1")
	err := ledger.Record("0xParticipant1", "usdc", decimal.NewFromInt(1000))
	require.NoError(t, err)

	// Create RPC request with token address parameter
	params := map[string]string{
		"acc": "0xParticipant1",
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	rpcRequest := &RPCRequest{
		Req: RPCData{
			RequestID: 1,
			Method:    "get_ledger_balances",
			Params:    []any{json.RawMessage(paramsJSON)},
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{"dummy-signature"},
	}

	// Use the test-specific handler instead of the actual one
	response, err := HandleGetLedgerBalances(rpcRequest, "0xParticipant1", db)
	require.NoError(t, err)
	assert.NotNil(t, response)

	// Extract the response data
	var responseParams []any
	responseParams = response.Res.Params
	require.NotEmpty(t, responseParams)

	// First parameter should be an array of ChannelAvailabilityResponse
	channelsArray, ok := responseParams[0].([]Balance)
	require.True(t, ok, "Response should contain an array of ChannelAvailabilityResponse")

	// We should have 4 channels with positive balances (excluding closed one)
	assert.Equal(t, 1, len(channelsArray), "Should have 4 channels")

	// Check the contents of each channel response
	expectedAssets := map[string]decimal.Decimal{
		"usdc": decimal.NewFromInt(1000),
	}

	for _, ch := range channelsArray {
		expectedBalance, exists := expectedAssets[ch.AssetSymbol]
		assert.True(t, exists, "Unexpected address in response: %s", ch.AssetSymbol)
		assert.Equal(t, expectedBalance, ch.Amount, "Incorrect balance for address %s", ch.AssetSymbol)

		// Remove from map to ensure each address appears only once
		delete(expectedAssets, ch.AssetSymbol)
	}

	assert.Empty(t, expectedAssets, "Not all expected addresses were found in the response")
}

// TestHandleGetConfig tests the get config handler functionality
func TestHandleGetConfig(t *testing.T) {
	rpcRequest := &RPCRequest{
		Req: RPCData{
			RequestID: 1,
			Method:    "get_config",
			Params:    []any{},
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{"dummy-signature"},
	}

	response, err := HandleGetConfig(rpcRequest)
	require.NoError(t, err)
	assert.NotNil(t, response)

	// Extract the response data
	var responseParams []any
	responseParams = response.Res.Params
	require.NotEmpty(t, responseParams)

	// First parameter should be a BrokerConfig
	configMap, ok := responseParams[0].(BrokerConfig)
	require.True(t, ok, "Response should contain a BrokerConfig")

	assert.Equal(t, BrokerAddress, configMap.BrokerAddress)
}

// TestHandleGetChannels tests the get channels functionality
func TestHandleGetChannels(t *testing.T) {
	rawKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	signer := Signer{privateKey: rawKey}
	participantAddr := signer.GetAddress().Hex()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	tokenAddress := "0xToken123"
	chainID := uint32(137)

	channels := []Channel{
		{
			ChannelID:   "0xChannel1",
			Participant: participantAddr,
			Status:      ChannelStatusOpen,
			Token:       tokenAddress + "1",
			ChainID:     chainID,
			Amount:      1000,
			Nonce:       1,
			Adjudicator: "0xAdj1",
			CreatedAt:   time.Now().Add(-24 * time.Hour), // 1 day ago
			UpdatedAt:   time.Now(),
		},
		{
			ChannelID:   "0xChannel2",
			Participant: participantAddr,
			Status:      ChannelStatusClosed,
			Token:       tokenAddress + "2",
			ChainID:     chainID,
			Amount:      2000,
			Nonce:       2,
			Adjudicator: "0xAdj2",
			CreatedAt:   time.Now().Add(-12 * time.Hour), // 12 hours ago
			UpdatedAt:   time.Now(),
		},
		{
			ChannelID:   "0xChannel3",
			Participant: participantAddr,
			Status:      ChannelStatusJoining,
			Token:       tokenAddress + "3",
			ChainID:     chainID,
			Amount:      3000,
			Nonce:       3,
			Adjudicator: "0xAdj3",
			CreatedAt:   time.Now().Add(-6 * time.Hour), // 6 hours ago
			UpdatedAt:   time.Now(),
		},
	}

	for _, channel := range channels {
		require.NoError(t, db.Create(&channel).Error)
	}

	otherChannel := Channel{
		ChannelID:   "0xOtherChannel",
		Participant: "0xOtherParticipant",
		Status:      ChannelStatusOpen,
		Token:       tokenAddress + "4",
		ChainID:     chainID,
		Amount:      5000,
		Nonce:       4,
		Adjudicator: "0xAdj4",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, db.Create(&otherChannel).Error)

	params := map[string]string{
		"participant": participantAddr,
	}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	rpcRequest := &RPCRequest{
		Req: RPCData{
			RequestID: 123,
			Method:    "get_channels",
			Params:    []any{json.RawMessage(paramsJSON)},
			Timestamp: uint64(time.Now().Unix()),
		},
	}

	reqBytes, err := json.Marshal(rpcRequest.Req)
	require.NoError(t, err)
	signed, err := signer.Sign(reqBytes)
	require.NoError(t, err)
	rpcRequest.Sig = []string{hexutil.Encode(signed)}

	response, err := HandleGetChannels(rpcRequest, db)
	require.NoError(t, err)
	require.NotNil(t, response)

	assert.Equal(t, "get_channels", response.Res.Method)
	assert.Equal(t, uint64(123), response.Res.RequestID)

	require.Len(t, response.Res.Params, 1, "Response should contain a slice of ChannelResponse")
	channelsSlice, ok := response.Res.Params[0].([]ChannelResponse)
	require.True(t, ok, "Response parameter should be a slice of ChannelResponse")

	// Should return all 3 channels for the participant
	assert.Len(t, channelsSlice, 3, "Should return all 3 channels for the participant")

	// Verify the channels are ordered by creation date (newest first)
	assert.Equal(t, "0xChannel3", channelsSlice[0].ChannelID, "First channel should be the newest")
	assert.Equal(t, "0xChannel2", channelsSlice[1].ChannelID, "Second channel should be the middle one")
	assert.Equal(t, "0xChannel1", channelsSlice[2].ChannelID, "Third channel should be the oldest")

	// Verify channel data is correct
	for _, ch := range channelsSlice {
		assert.Equal(t, participantAddr, ch.Participant, "ParticipantA should match")
		// Token now has a suffix, so we check it starts with the base token address
		assert.True(t, strings.HasPrefix(ch.Token, tokenAddress), "Token should start with the base token address")
		assert.Equal(t, chainID, ch.ChainID, "NetworkID should match")

		// Find the corresponding original channel to compare with
		var originalChannel Channel
		for _, c := range channels {
			if c.ChannelID == ch.ChannelID {
				originalChannel = c
				break
			}
		}

		assert.Equal(t, originalChannel.Status, ch.Status, "Status should match")
		assert.Equal(t, originalChannel.Amount, ch.Amount, "Amount should match")
		assert.NotEmpty(t, ch.CreatedAt, "CreatedAt should not be empty")
		assert.NotEmpty(t, ch.UpdatedAt, "UpdatedAt should not be empty")
	}

	// Test with invalid signature
	invalidReq := &RPCRequest{
		Req: RPCData{
			RequestID: 456,
			Method:    "get_channels",
			Params:    []any{json.RawMessage(paramsJSON)},
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{"0xInvalidSignature"},
	}

	_, err = HandleGetChannels(invalidReq, db)
	assert.Error(t, err, "Should return error with invalid signature")
	assert.Contains(t, err.Error(), "invalid signature", "Error should mention invalid signature")

	// Test with missing participant parameter
	missingParamReq := &RPCRequest{
		Req: RPCData{
			RequestID: 789,
			Method:    "get_channels",
			Params:    []any{map[string]string{}}, // Empty map
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{hexutil.Encode(signed)},
	}

	_, err = HandleGetChannels(missingParamReq, db)
	assert.Error(t, err, "Should return error with missing participant")
	assert.Contains(t, err.Error(), "missing participant", "Error should mention missing participant")
}

func TestHandleGetRPCHistory(t *testing.T) {
	rawKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	signer := Signer{privateKey: rawKey}
	participantAddr := signer.GetAddress().Hex()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	rpcStore := NewRPCStore(db)

	timestamp := uint64(time.Now().Unix())
	records := []RPCRecord{
		{
			Sender:    participantAddr,
			ReqID:     1,
			Method:    "ping",
			Params:    []byte(`[null]`),
			Timestamp: timestamp - 3600, // 1 hour ago
			ReqSig:    []string{"sig1"},
			Response:  []byte(`{"res":[1,"pong",[],1621234567890]}`),
			ResSig:    []string{},
		},
		{
			Sender:    participantAddr,
			ReqID:     2,
			Method:    "get_config",
			Params:    []byte(`[]`),
			Timestamp: timestamp - 1800, // 30 minutes ago
			ReqSig:    []string{"sig2"},
			Response:  []byte(`{"res":[2,"get_config",[{"broker_address":"0xBroker"}],1621234597890]}`),
			ResSig:    []string{},
		},
		{
			Sender:    participantAddr,
			ReqID:     3,
			Method:    "get_channels",
			Params:    []byte(`[{"participant":"` + participantAddr + `"}]`),
			Timestamp: timestamp - 900, // 15 minutes ago
			ReqSig:    []string{"sig3"},
			Response:  []byte(`{"res":[3,"get_channels",[[]]],1621234627890]}`),
			ResSig:    []string{},
		},
	}

	for _, record := range records {
		require.NoError(t, db.Create(&record).Error)
	}

	otherRecord := RPCRecord{
		Sender:    "0xOtherParticipant",
		ReqID:     4,
		Method:    "ping",
		Params:    []byte(`[null]`),
		Timestamp: timestamp,
		ReqSig:    []string{"sig4"},
		Response:  []byte(`{"res":[4,"pong",[],1621234657890]}`),
		ResSig:    []string{},
	}
	require.NoError(t, db.Create(&otherRecord).Error)

	rpcRequest := &RPCRequest{
		Req: RPCData{
			RequestID: 100,
			Method:    "get_rpc_history",
			Params:    []any{},
			Timestamp: timestamp,
		},
	}

	reqBytes, err := json.Marshal(rpcRequest.Req)
	require.NoError(t, err)
	signed, err := signer.Sign(reqBytes)
	require.NoError(t, err)
	rpcRequest.Sig = []string{hexutil.Encode(signed)}

	response, err := HandleGetRPCHistory(participantAddr, rpcRequest, rpcStore)
	require.NoError(t, err)
	require.NotNil(t, response)

	assert.Equal(t, "get_rpc_history", response.Res.Method)
	assert.Equal(t, uint64(100), response.Res.RequestID)

	require.Len(t, response.Res.Params, 1, "Response should contain a RPCHistoryResponse")
	rpcHistory, ok := response.Res.Params[0].([]RPCEntry)
	require.True(t, ok, "Response parameter should be a RPCHistoryResponse")

	assert.Len(t, rpcHistory, 3, "Should return 3 records for the participant")

	assert.Equal(t, uint64(3), rpcHistory[0].ReqID, "First record should be the newest")
	assert.Equal(t, uint64(2), rpcHistory[1].ReqID, "Second record should be the middle one")
	assert.Equal(t, uint64(1), rpcHistory[2].ReqID, "Third record should be the oldest")

	missingParamReq := &RPCRequest{
		Req: RPCData{
			RequestID: 789,
			Method:    "get_rpc_history",
			Params:    []any{},
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{hexutil.Encode(signed)},
	}

	_, err = HandleGetRPCHistory("", missingParamReq, rpcStore)
	assert.Error(t, err, "Should return error with missing participant")
	assert.Contains(t, err.Error(), "missing participant", "Error should mention missing participant")
}
