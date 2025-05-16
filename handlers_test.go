package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
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
	if err != nil {
		t.Fatalf("could not generate secp256k1 key: %v", err)
	}

	signer := Signer{
		privateKey: raw,
	}
	addr := signer.GetAddress()
	participantA := addr.Hex()

	// Set up test database with cleanup
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create ledger
	ledger := NewLedger(db)

	// Create token address
	tokenAddress := "0xToken123"

	// Set up participants
	participantB := "0xParticipantB"

	// Create channels for both participants
	channelA := &Channel{
		ChannelID:    "0xChannelA",
		ParticipantA: participantA,
		ParticipantB: BrokerAddress,
		Status:       ChannelStatusOpen,
		Token:        tokenAddress,
		Nonce:        1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, db.Create(channelA).Error)

	channelB := &Channel{
		ChannelID:    "0xChannelB",
		ParticipantA: participantB,
		ParticipantB: BrokerAddress,
		Status:       ChannelStatusOpen,
		Token:        tokenAddress,
		Nonce:        1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, db.Create(channelB).Error)

	// Create a virtual app
	vAppID := "0xVApp123"
	vApp := &AppSession{
		AppID:        vAppID,
		Participants: []string{participantA, participantB},
		Status:       ChannelStatusOpen,
		Challenge:    60,
		Weights:      []int64{100, 0},
		Token:        tokenAddress,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Quorum:       100,
	}
	require.NoError(t, db.Create(vApp).Error)

	// Add funds to the virtual app
	accountA := ledger.SelectBeneficiaryAccount(vAppID, participantA)
	require.NoError(t, accountA.Record(200))

	accountB := ledger.SelectBeneficiaryAccount(vAppID, participantB)
	require.NoError(t, accountB.Record(300))

	closeParams := CloseAppSessionParams{
		AppSessionID: vAppID,
		Allocations:  []int64{250, 250},
	}

	// Create RPC request
	paramsJSON, err := json.Marshal(closeParams)
	require.NoError(t, err)

	req := &RPCRequest{
		Req: RPCData{
			RequestID: 1,
			Method:    "close_app_session",
			Params:    []any{json.RawMessage(paramsJSON)},
			Timestamp: uint64(time.Now().Unix()),
		},
	}

	// Create signing data
	closeSignData := CloseAppSignData{
		RequestID: req.Req.RequestID,
		Method:    req.Req.Method,
		Params:    []CloseAppSessionParams{closeParams},
		Timestamp: req.Req.Timestamp,
	}
	signBytes, err := json.Marshal(closeSignData)
	require.NoError(t, err)

	signed, err := signer.Sign(signBytes)
	require.NoError(t, err)
	req.Sig = []string{hexutil.Encode(signed)}

	resp, err := HandleCloseApplication(req, ledger)
	require.NoError(t, err)

	// Verify response
	assert.Equal(t, "close_app_session", resp.Res.Method)
	assert.Equal(t, uint64(1), resp.Res.RequestID)

	// Check that channel is marked as closed
	var updatedChannel AppSession
	require.NoError(t, db.Where("app_id = ?", vAppID).First(&updatedChannel).Error)
	assert.Equal(t, ChannelStatusClosed, updatedChannel.Status)

	// Check that funds were transferred back to channels according to allocations
	directAccountA := ledger.SelectBeneficiaryAccount(channelA.ChannelID, participantA)
	balanceA, err := directAccountA.Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(250), balanceA)

	directAccountB := ledger.SelectBeneficiaryAccount(channelB.ChannelID, participantB)
	balanceB, err := directAccountB.Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(250), balanceB)

	// Check that virtual app accounts are empty
	virtualAccountA := ledger.SelectBeneficiaryAccount(vAppID, participantA)
	virtualBalanceA, err := virtualAccountA.Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(0), virtualBalanceA)

	virtualAccountB := ledger.SelectBeneficiaryAccount(vAppID, participantB)
	virtualBalanceB, err := virtualAccountB.Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(0), virtualBalanceB)
}

// TestHandleCreateVirtualApp tests the create virtual app handler functionality
func TestHandleCreateVirtualApp(t *testing.T) {
	// Generate private keys for both participants
	rawKeyA, err := crypto.GenerateKey()
	require.NoError(t, err)
	signerA := Signer{privateKey: rawKeyA}
	addrA := signerA.GetAddress().Hex()

	rawKeyB, err := crypto.GenerateKey()
	require.NoError(t, err)
	signerB := Signer{privateKey: rawKeyB}
	addrB := signerB.GetAddress().Hex()

	// Set up test database with cleanup
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create channels for both participants
	tokenAddress := "0xTokenXYZ"
	channelA := &Channel{
		ChannelID:    "0xChannelA",
		ParticipantA: addrA,
		ParticipantB: BrokerAddress,
		Status:       ChannelStatusOpen,
		Token:        tokenAddress,
		Nonce:        1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, db.Create(channelA).Error)

	channelB := &Channel{
		ChannelID:    "0xChannelB",
		ParticipantA: addrB,
		ParticipantB: BrokerAddress,
		Status:       ChannelStatusOpen,
		Token:        tokenAddress,
		Nonce:        1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, db.Create(channelB).Error)

	// Create ledger and fund channels
	ledger := NewLedger(db)
	acctA := ledger.SelectBeneficiaryAccount(channelA.ChannelID, addrA)
	require.NoError(t, acctA.Record(100))
	acctB := ledger.SelectBeneficiaryAccount(channelB.ChannelID, addrB)
	require.NoError(t, acctB.Record(200))

	// Create common timestamp for all signatures - will also be used as nonce
	timestamp := uint64(time.Now().Unix())

	// First set up the combined parameters in the same format as the handler uses
	appDefinition := AppDefinition{
		Protocol:     "test-proto",
		Participants: []string{addrA, addrB},
		Weights:      []uint64{1, 1},
		Quorum:       2,
		Challenge:    60,
		Nonce:        timestamp, // Set nonce to match what the handler sets
	}

	// Create the RPC request with the combined application parameters
	createParams := CreateAppSessionParams{
		Definition:  appDefinition,
		Token:       tokenAddress,
		Allocations: []int64{100, 200}, // Combined allocations
	}

	rpcReq := &RPCRequest{
		Req: RPCData{
			RequestID: 42,
			Method:    "create_app_session",
			Params:    []any{createParams},
			Timestamp: timestamp,
		},
		Intent: []int64{100, 200},
	}

	// Create the CreateAppSignData object exactly as it's created in HandleCreateApplication
	// This is the critical part to match!
	req := CreateAppSignData{
		RequestID: rpcReq.Req.RequestID,
		Method:    rpcReq.Req.Method,
		Params:    []CreateAppSessionParams{createParams},
		Timestamp: rpcReq.Req.Timestamp,
	}

	// Important: Use the custom MarshalJSON method instead of standard json.Marshal
	// This ensures the exact same data format as in the handler
	reqBytes, err := req.MarshalJSON()
	require.NoError(t, err)

	// Sign with participant A's key
	signA, err := signerA.Sign(reqBytes)
	require.NoError(t, err)
	sigA := hexutil.Encode(signA)

	// Sign with participant B's key
	signB, err := signerB.Sign(reqBytes)
	require.NoError(t, err)
	sigB := hexutil.Encode(signB)

	// Add both signatures to the request
	rpcReq.Sig = []string{sigA, sigB}

	// Process the request
	resp, err := HandleCreateApplication(rpcReq, ledger)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Validate RPC response
	assert.Equal(t, rpcReq.Req.Method, resp.Res.Method)
	assert.Equal(t, uint64(42), resp.Res.RequestID)

	// Extract the AppResponse
	params := resp.Res.Params
	require.Len(t, params, 1)
	require.IsType(t, &AppSessionResponse{}, params[0])
	appResp := params[0].(*AppSessionResponse)

	assert.Equal(t, string(ChannelStatusOpen), appResp.Status)

	// Verify the VApp record exists
	var vApp AppSession
	require.NoError(t, db.
		Where("app_id = ?", appResp.AppSessionID).
		First(&vApp).Error)
	assert.Equal(t, tokenAddress, vApp.Token)
	assert.ElementsMatch(t, []string{addrA, addrB}, vApp.Participants)
	assert.Equal(t, ChannelStatusOpen, vApp.Status)

	// Check balances: channels drained, virtual app funded
	directBalA, err := ledger.SelectBeneficiaryAccount(channelA.ChannelID, addrA).Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(0), directBalA, "channel A should be drained")

	directBalB, err := ledger.SelectBeneficiaryAccount(channelB.ChannelID, addrB).Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(0), directBalB, "channel B should be drained")

	virtBalA, err := ledger.SelectBeneficiaryAccount(appResp.AppSessionID, addrA).Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(100), virtBalA, "virtual app A balance")

	virtBalB, err := ledger.SelectBeneficiaryAccount(appResp.AppSessionID, addrB).Balance()
	require.NoError(t, err)
	assert.Equal(t, int64(200), virtBalB, "virtual app B balance")
}

// TestHandleListParticipants tests the list available channels handler functionality
func TestHandleListParticipants(t *testing.T) {
	// Set up test database with cleanup
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create channel service and ledger
	ledger := NewLedger(db)

	// Create test channels with the broker
	participants := []struct {
		address        string
		channelID      string
		initialBalance int64
		status         ChannelStatus
	}{
		{"0xParticipant1", "0xChannel1", 1000, ChannelStatusOpen},
	}

	// Insert channels and ledger entries for testing
	for _, p := range participants {
		// Create channel
		channel := Channel{
			ChannelID:    p.channelID,
			ParticipantA: p.address,
			ParticipantB: BrokerAddress,
			Status:       p.status,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		err := db.Create(&channel).Error
		require.NoError(t, err)

		// Add funds if needed
		if p.initialBalance > 0 {
			account := ledger.SelectBeneficiaryAccount(p.channelID, p.address)
			err = account.Record(p.initialBalance)
			require.NoError(t, err)
		}
	}

	// Create RPC request with token address parameter
	params := map[string]string{
		"acc": "0xChannel1",
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
	response, err := HandleGetLedgerBalances(rpcRequest, ledger)
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
	expectedAddresses := map[string]int64{
		"0xParticipant1": 1000,
	}

	for _, ch := range channelsArray {
		expectedBalance, exists := expectedAddresses[ch.Asset]
		assert.True(t, exists, "Unexpected address in response: %s", ch.Asset)
		assert.Equal(t, expectedBalance, ch.Amount, "Incorrect balance for address %s", ch.Asset)

		// Remove from map to ensure each address appears only once
		delete(expectedAddresses, ch.Asset)
	}

	assert.Empty(t, expectedAddresses, "Not all expected addresses were found in the response")
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

	ledger := NewLedger(db)

	tokenAddress := "0xToken123"
	networkID := "137"

	channels := []Channel{
		{
			ChannelID:    "0xChannel1",
			ParticipantA: participantAddr,
			ParticipantB: BrokerAddress,
			Status:       ChannelStatusOpen,
			Token:        tokenAddress,
			NetworkID:    networkID,
			Amount:       1000,
			Nonce:        1,
			CreatedAt:    time.Now().Add(-24 * time.Hour), // 1 day ago
			UpdatedAt:    time.Now(),
		},
		{
			ChannelID:    "0xChannel2",
			ParticipantA: participantAddr,
			ParticipantB: BrokerAddress,
			Status:       ChannelStatusClosed,
			Token:        tokenAddress,
			NetworkID:    networkID,
			Amount:       2000,
			Nonce:        2,
			CreatedAt:    time.Now().Add(-12 * time.Hour), // 12 hours ago
			UpdatedAt:    time.Now(),
		},
		{
			ChannelID:    "0xChannel3",
			ParticipantA: participantAddr,
			ParticipantB: BrokerAddress,
			Status:       ChannelStatusJoining,
			Token:        tokenAddress,
			NetworkID:    networkID,
			Amount:       3000,
			Nonce:        3,
			CreatedAt:    time.Now().Add(-6 * time.Hour), // 6 hours ago
			UpdatedAt:    time.Now(),
		},
	}

	for _, channel := range channels {
		require.NoError(t, db.Create(&channel).Error)
	}

	otherChannel := Channel{
		ChannelID:    "0xOtherChannel",
		ParticipantA: "0xOtherParticipant",
		ParticipantB: BrokerAddress,
		Status:       ChannelStatusOpen,
		Token:        tokenAddress,
		NetworkID:    networkID,
		Amount:       5000,
		Nonce:        4,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
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

	response, err := HandleGetChannels(rpcRequest, ledger)
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
		assert.Equal(t, tokenAddress, ch.Token, "Token should match")
		assert.Equal(t, networkID, ch.NetworkID, "NetworkID should match")

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

	_, err = HandleGetChannels(invalidReq, ledger)
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

	_, err = HandleGetChannels(missingParamReq, ledger)
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
