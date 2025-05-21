package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/erc7824/go-nitrolite"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
)

const (
	keyFileName = "signer_key.hex"
)

// Signer handles signing operations using a private key
type Signer struct {
	privateKey *ecdsa.PrivateKey
}

// RPCMessage represents a complete message in the RPC protocol, including data and signatures
type RPCMessage struct {
	Req          *RPCData `json:"req,omitempty"`
	Sig          []string `json:"sig"`
	AppSessionID string   `json:"sid,omitempty"`
}

// RPCData represents the common structure for both requests and responses
// Format: [request_id, method, params, ts]
type RPCData struct {
	RequestID uint64 `json:"id"`
	Method    string `json:"method"`
	Params    []any  `json:"params"`
	Timestamp uint64 `json:"ts"`
}

// MarshalJSON implements the json.Marshaler interface for RPCData
func (m RPCData) MarshalJSON() ([]byte, error) {
	// Create array representation
	return json.Marshal([]any{
		m.RequestID,
		m.Method,
		m.Params,
		m.Timestamp,
	})
}

// NewSigner creates a new signer from a hex-encoded private key
func NewSigner(privateKeyHex string) (*Signer, error) {
	if len(privateKeyHex) >= 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, err
	}

	return &Signer{privateKey: privateKey}, nil
}

// Sign creates an ECDSA signature for the provided data
func (s *Signer) Sign(data []byte) ([]byte, error) {
	sig, err := nitrolite.Sign(data, s.privateKey)
	if err != nil {
		return nil, err
	}

	signature := make([]byte, 65)
	copy(signature[0:32], sig.R[:])
	copy(signature[32:64], sig.S[:])

	if sig.V >= 27 {
		signature[64] = sig.V - 27
	}
	return signature, nil
}

// GetAddress returns the address derived from the signer's public key
func (s *Signer) GetAddress() string {
	publicKey := s.privateKey.Public().(*ecdsa.PublicKey)
	return crypto.PubkeyToAddress(*publicKey).Hex()
}

// generatePrivateKey generates a new private key
func generatePrivateKey() (*ecdsa.PrivateKey, error) {
	return crypto.GenerateKey()
}

// savePrivateKey saves a private key to file
func savePrivateKey(key *ecdsa.PrivateKey, filePath string) error {
	keyBytes := crypto.FromECDSA(key)
	keyHex := hexutil.Encode(keyBytes)
	// Remove "0x" prefix
	if len(keyHex) >= 2 && keyHex[:2] == "0x" {
		keyHex = keyHex[2:]
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return ioutil.WriteFile(filePath, []byte(keyHex), 0600)
}

// loadPrivateKey loads a private key from file
func loadPrivateKey(filePath string) (*ecdsa.PrivateKey, error) {
	keyHex, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return crypto.HexToECDSA(string(keyHex))
}

// getOrCreatePrivateKey gets an existing private key or creates a new one
func getOrCreatePrivateKey(keyPath string) (*ecdsa.PrivateKey, error) {
	if _, err := os.Stat(keyPath); err == nil {
		// Key file exists, load it
		key, err := loadPrivateKey(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load existing key: %w", err)
		}
		return key, nil
	}

	// Generate new key
	key, err := generatePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate new key: %w", err)
	}

	// Save the key
	if err := savePrivateKey(key, keyPath); err != nil {
		return nil, fmt.Errorf("failed to save new key: %w", err)
	}

	return key, nil
}

// Client handles websocket connection and RPC messaging
type Client struct {
	conn    *websocket.Conn
	signer  *Signer
	address string
}

// NewClient creates a new websocket client
func NewClient(serverURL string, signer *Signer) (*Client, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	return &Client{
		conn:    conn,
		signer:  signer,
		address: signer.GetAddress(),
	}, nil
}

// SendMessage sends an RPC message to the server
func (c *Client) SendMessage(rpcMsg RPCMessage) error {
	// Marshal the message to JSON
	data, err := json.Marshal(rpcMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Send the message
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Authenticate performs the authentication flow with the server
func (c *Client) Authenticate() error {
	fmt.Println("Starting authentication...")

	// Step 1: Auth request
	authReq := RPCMessage{
		Req: &RPCData{
			RequestID: 1,
			Method:    "auth_request",
			Params:    []any{c.address},
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{},
	}

	// Sign the request
	reqData, err := json.Marshal(authReq.Req)
	if err != nil {
		return fmt.Errorf("failed to marshal auth request: %w", err)
	}

	signature, err := c.signer.Sign(reqData)
	if err != nil {
		return fmt.Errorf("failed to sign auth request: %w", err)
	}
	authReq.Sig = []string{hexutil.Encode(signature)}

	// Send auth request
	if err := c.SendMessage(authReq); err != nil {
		return fmt.Errorf("failed to send auth request: %w", err)
	}

	// Step 2: Receive challenge
	fmt.Println("Waiting for challenge...")
	_, challengeMsg, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read challenge response: %w", err)
	}

	var challengeResp map[string]any
	if err := json.Unmarshal(challengeMsg, &challengeResp); err != nil {
		return fmt.Errorf("failed to parse challenge response: %w", err)
	}

	// Extract challenge from response
	resArray, ok := challengeResp["res"].([]any)
	if !ok || len(resArray) < 3 {
		return fmt.Errorf("invalid challenge response format")
	}

	paramsArray, ok := resArray[2].([]any)
	if !ok || len(paramsArray) < 1 {
		return fmt.Errorf("invalid challenge parameters")
	}

	challengeObj, ok := paramsArray[0].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid challenge object")
	}

	challengeStr, ok := challengeObj["challenge_message"].(string)
	if !ok {
		return fmt.Errorf("missing challenge message")
	}

	// Step 3: Send auth verify
	fmt.Println("Sending challenge verification...")
	verifyReq := RPCMessage{
		Req: &RPCData{
			RequestID: 2,
			Method:    "auth_verify",
			Params: []any{map[string]any{
				"address":   c.address,
				"challenge": challengeStr,
			}},
			Timestamp: uint64(time.Now().Unix()),
		},
		Sig: []string{},
	}

	// Sign the verify request
	verifyData, err := json.Marshal(verifyReq.Req)
	if err != nil {
		return fmt.Errorf("failed to marshal verify request: %w", err)
	}

	verifySignature, err := c.signer.Sign(verifyData)
	if err != nil {
		return fmt.Errorf("failed to sign verify request: %w", err)
	}
	verifyReq.Sig = []string{hexutil.Encode(verifySignature)}

	// Send verify request
	if err := c.SendMessage(verifyReq); err != nil {
		return fmt.Errorf("failed to send verify request: %w", err)
	}

	// Receive auth verify response
	_, verifyMsg, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read verify response: %w", err)
	}

	var verifyResp map[string]any
	if err := json.Unmarshal(verifyMsg, &verifyResp); err != nil {
		return fmt.Errorf("failed to parse verify response: %w", err)
	}

	// Check if auth was successful
	resVerifyArray, ok := verifyResp["res"].([]any)
	if !ok || len(resVerifyArray) < 3 {
		return fmt.Errorf("invalid verify response format")
	}

	verifyParamsArray, ok := resVerifyArray[2].([]any)
	if !ok || len(verifyParamsArray) < 1 {
		return fmt.Errorf("invalid verify parameters")
	}

	verifyObj, ok := verifyParamsArray[0].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid verify object")
	}

	success, ok := verifyObj["success"].(bool)
	if !ok || !success {
		return fmt.Errorf("authentication failed")
	}

	fmt.Println("Authentication successful!")
	return nil
}

// Close closes the websocket connection
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func main() {
	// Define flags
	var (
		methodFlag = flag.String("method", "", "RPC method name")
		idFlag     = flag.Uint64("id", 1, "Request ID")
		paramsFlag = flag.String("params", "[]", "JSON array of parameters")
		sendFlag   = flag.Bool("send", false, "Send the message to the server")
		serverFlag = flag.String("server", "ws://localhost:8000/ws", "WebSocket server URL")
		genKeyFlag = flag.Bool("genkey", false, "Generate a new private key and exit")
	)

	flag.Parse()
	
	// If genkey flag is set, generate a private key and exit
	if *genKeyFlag {
		currentDir, err := os.Getwd()
		if err != nil {
			log.Fatalf("Error getting current directory: %v", err)
		}
		keyPath := filepath.Join(currentDir, keyFileName)
		
		// Generate new key
		key, err := generatePrivateKey()
		if err != nil {
			log.Fatalf("Error generating private key: %v", err)
		}
		
		// Save the key
		if err := savePrivateKey(key, keyPath); err != nil {
			log.Fatalf("Error saving private key: %v", err)
		}
		
		// Create signer to display address
		signer, err := NewSigner(hexutil.Encode(crypto.FromECDSA(key)))
		if err != nil {
			log.Fatalf("Error creating signer: %v", err)
		}
		
		fmt.Printf("Generated new private key at: %s\n", keyPath)
		fmt.Printf("Ethereum Address: %s\n", signer.GetAddress())
		
		// Read and display the key for convenience
		keyHex, err := ioutil.ReadFile(keyPath)
		if err != nil {
			log.Fatalf("Error reading key file: %v", err)
		}
		fmt.Printf("Private Key (add 0x prefix for MetaMask): %s\n", string(keyHex))
		
		os.Exit(0)
	}

	// For normal operation, method is required
	if *methodFlag == "" {
		fmt.Println("Error: method is required")
		flag.Usage()
		os.Exit(1)
	}

	// Parse params
	var params []any
	if err := json.Unmarshal([]byte(*paramsFlag), &params); err != nil {
		log.Fatalf("Error parsing params JSON: %v", err)
	}

	// Get or create private key
	// Use the current directory to store the key file
	currentDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error getting current directory: %v", err)
	}
	keyPath := filepath.Join(currentDir, keyFileName)
	privateKey, err := getOrCreatePrivateKey(keyPath)
	if err != nil {
		log.Fatalf("Error with private key: %v", err)
	}

	// Create signer
	signer, err := NewSigner(hexutil.Encode(crypto.FromECDSA(privateKey)))
	if err != nil {
		log.Fatalf("Error creating signer: %v", err)
	}

	// Show address for reference
	fmt.Printf("Using address: %s\n", signer.GetAddress())

	// Create RPC data
	rpcData := RPCData{
		RequestID: *idFlag,
		Method:    *methodFlag,
		Params:    params,
		Timestamp: uint64(time.Now().Unix()),
	}

	// Serialize RPC data for signing
	dataBytes, err := json.Marshal(rpcData)
	if err != nil {
		log.Fatalf("Error marshaling RPC data: %v", err)
	}

	// Sign the data
	signature, err := signer.Sign(dataBytes)
	if err != nil {
		log.Fatalf("Error signing data: %v", err)
	}

	// Create final RPC message with signature
	rpcMessage := RPCMessage{
		Req: &rpcData,
		Sig: []string{hexutil.Encode(signature)},
	}

	// Output the final message
	output, err := json.MarshalIndent(rpcMessage, "", "  ")
	if err != nil {
		log.Fatalf("Error marshaling final message: %v", err)
	}

	fmt.Println(string(output))

	// If send flag is set, send the message to the server
	if *sendFlag {
		client, err := NewClient(*serverFlag, signer)
		if err != nil {
			log.Fatalf("Error creating client: %v", err)
		}
		defer client.Close()

		// Authenticate with the server
		if err := client.Authenticate(); err != nil {
			log.Fatalf("Authentication failed: %v", err)
		}

		// Send the message
		if err := client.SendMessage(rpcMessage); err != nil {
			log.Fatalf("Error sending message: %v", err)
		}

		// Wait for response
		_, respMsg, err := client.conn.ReadMessage()
		if err != nil {
			log.Fatalf("Error reading response: %v", err)
		}

		// Pretty print the response
		var respObj map[string]any
		if err := json.Unmarshal(respMsg, &respObj); err != nil {
			log.Fatalf("Error parsing response: %v", err)
		}

		respOut, err := json.MarshalIndent(respObj, "", "  ")
		if err != nil {
			log.Fatalf("Error marshaling response: %v", err)
		}

		fmt.Println("\nServer response:")
		fmt.Println(string(respOut))
	}
}
