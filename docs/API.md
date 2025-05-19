# Clearnode API Reference

## API Endpoints

| Method | Description |
|--------|-------------|
| `auth_request` | Initiates authentication with the server |
| `auth_challenge` | Server response with authentication challenge |
| `auth_verify` | Completes authentication with a challenge response |
| `ping` | Simple connectivity check |
| `get_config` | Retrieves broker configuration |
| `get_app_definition` | Retrieves application definition for a ledger account |
| `get_ledger_balances` | Lists participants and their balances for a ledger account |
| `get_channels` | Lists all channels for a participant with their status |
| `get_rpc_history` | Retrieves all RPC message history for a participant |
| `create_app_session` | Creates a new virtual application on a ledger |
| `close_app_session` | Closes a virtual application |
| `close_channel` | Closes a payment channel |
| `resize_channel` | Adjusts channel capacity |
| `message` | Sends a message to all participants in a virtual application |

## Authentication

### Authentication Request

Initiates authentication with the server.

**Request:**

```json
{
  "req": [1, "auth_request", ["0x1234567890abcdef..."], 1619123456789],
  "sig": ["0x5432abcdef..."] // Client's signature of the entire 'req' object
}
```

### Authentication Challenge

Server response with a challenge token for the client to sign.

**Response:**

```json
{
  "res": [1, "auth_challenge", [{
    "challenge_message": "550e8400-e29b-41d4-a716-446655440000"
  }], 1619123456789],
  "sig": ["0x9876fedcba..."] // Server's signature of the entire 'res' object
}
```

### Authentication Verification

Completes authentication with a challenge response.

**Request:**

```json
{
  "req": [2, "auth_verify", [{
    "address": "0x1234567890abcdef...",
    "challenge": "550e8400-e29b-41d4-a716-446655440000"
  }], 1619123456789],
  "sig": ["0x2345bcdef..."] // Client's signature of the entire 'req' object
}
```

**Response:**

```json
{
  "res": [2, "auth_verify", [{
    "address": "0x1234567890abcdef...",
    "success": true
  }], 1619123456789],
  "sig": ["0xabcd1234..."] // Server's signature of the entire 'res' object
}
```

## Ledger Management

### Get App Definition

Retrieves the application definition for a specific ledger account.

**Request:**

```json
{
  "req": [2, "get_app_definition", [{
    "acc": "0x1234567890abcdef..."
  }], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [2, "get_app_definition", [
    {
      "protocol": "NitroRPC/0.2",
      "participants": [
        "0xAaBbCcDdEeFf0011223344556677889900aAbBcC",
        "0x00112233445566778899AaBbCcDdEeFf00112233"
      ],
      "weights": [50, 50],
      "quorum": 100,
      "challenge": 86400,
      "nonce": 1
    }
  ], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

### Get Ledger Balances

Retrieves the balances of all participants in a specific ledger account.

**Request:**

```json
{
  "req": [2, "get_ledger_balances", [{
    "acc": "0x1234567890abcdef..."
  }], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [2, "get_ledger_balances", [[
    {
      "address": "0x1234567890abcdef...",
      "amount": 100000
    },
    {
      "address": "0x2345678901abcdef...",
      "amount": 200000
    }
  ]], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

### Get Channels

Retrieves all channels for a participant (both open, closed, and joining), ordered by creation date (newest first).

**Request:**

```json
{
  "req": [3, "get_channels", [{
    "participant": "0x1234567890abcdef..."
  }], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [3, "get_channels", [[
    {
      "channel_id": "0xfedcba9876543210...",
      "participant": "0x1234567890abcdef...",
      "status": "open",
      "token": "0xeeee567890abcdef...",
      "amount": 100000,
      "network_id": "137",
      "created_at": "2023-05-01T12:00:00Z",
      "updated_at": "2023-05-01T12:30:00Z"
    },
    {
      "channel_id": "0xabcdef1234567890...",
      "participant": "0x1234567890abcdef...",
      "status": "closed",
      "token": "0xeeee567890abcdef...",
      "amount": 50000,
      "network_id": "137",
      "created_at": "2023-04-15T10:00:00Z",
      "updated_at": "2023-04-20T14:30:00Z"
    }
  ]], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

The signature in the request must be from the participant's private key, verifying they own the address. This prevents unauthorized access to channel information.

Each channel response includes:
- `channel_id`: Unique identifier for the channel
- `participant`: The participant's address (always the user who created the channel)
- `status`: Current status ("open", "closed", or "joining")
- `token`: The token address for the channel
- `amount`: Total channel capacity
- `network_id`: The blockchain network ID where the channel exists
- `created_at`: When the channel was created (ISO 8601 format)
- `updated_at`: When the channel was last updated (ISO 8601 format)

### Get RPC History

Retrieves all RPC messages history for a participant, ordered by timestamp (newest first).

**Request:**

```json
{
  "req": [4, "get_rpc_history", [{
    "participant": "0x1234567890abcdef..."
  }], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [4, "get_rpc_history", [[
    {
      "id": 123,
      "sender_address": "0x1234567890abcdef...",
      "req_id": 42,
      "method": "get_channels",
      "params": "[{\"participant\":\"0x1234567890abcdef...\"}]",
      "timestamp": 1619123456789,
      "req_sig": ["0x9876fedcba..."],
      "result": "{\"res\":[42,\"get_channels\",[[...]],1619123456799]}",
      "res_sig": ["0xabcd1234..."]
    },
    {
      "id": 122,
      "sender_address": "0x1234567890abcdef...",
      "req_id": 41,
      "method": "ping",
      "params": "[null]",
      "timestamp": 1619123446789,
      "req_sig": ["0x8765fedcba..."],
      "result": "{\"res\":[41,\"pong\",[],1619123446799]}",
      "res_sig": ["0xdcba4321..."]
    }
  ]], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

The signature in the request must be from the participant's private key, verifying they own the address. This prevents unauthorized access to message history.

Each RPC record includes:
- `id`: Database ID for the record
- `sender_address`: The participant's address that sent the request
- `req_id`: The original request ID
- `method`: The RPC method that was called
- `params`: The parameters that were sent with the request (as a JSON string)
- `timestamp`: When the request was received
- `req_sig`: The signature(s) that were included with the request
- `result`: The response that was sent back (as a JSON string)
- `res_sig`: The signature(s) that were included with the response

## Virtual Application Management

### Create Virtual Application

Creates a virtual application between participants.

**Request:**

```json
{
  "req": [3, "create_app_session", [{
    "definition": {
      "protocol": "NitroRPC/0.2",
      "participants": [
        "0xAaBbCcDdEeFf0011223344556677889900aAbBcC",
        "0x00112233445566778899AaBbCcDdEeFf00112233"
      ],
      "weights": [50, 50],
      "quorum": 100,
      "challenge": 86400,
      "nonce": 1
    },
    "token": "0xTokenAddress",
    "allocations": [100, 100]
  }], 1619123456789],
  "int": [100, 100], // Initial funding intent from 0, 0
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [3, "create_app_session", [{
    "app_id": "0x3456789012abcdef...",
    "status": "open"
  }], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

### Close Virtual Application

Closes a virtual application and redistributes funds.

**Request:**

```json
{
  "req": [4, "close_app_session", [{
    "app_id": "0x3456789012abcdef...",
    "allocations": [0, 200]
  }], 1619123456789],
  "int": [0, 200],
  "sig": ["0x9876fedcba...", "0x8765fedcba..."]
}
```

**Response:**

```json
{
  "res": [4, "close_app_session", [{
    "app_id": "0x3456789012abcdef...",
    "status": "closed"
  }], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

### Close Channel

Closes a channel between a participant and the broker.

**Request:**

```json
{
  "req": [5, "close_channel", [{
    "channel_id": "0x4567890123abcdef...",
    "funds_destination": "0x1234567890abcdef..."
  }], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [5, "close_channel", [{
    "channel_id": "0x4567890123abcdef...",
    "intent": 1,
    "version": "123",
    "state_data": "0x0000000000000000000000000000000000000000000000000000000000001ec7",
    "allocations": [
      {
        "destination": "0x1234567890abcdef...",
        "token": "0xeeee567890abcdef...",
        "amount": "50000"
      },
      {
        "destination": "0xbbbb567890abcdef...", // Broker address
        "token": "0xeeee567890abcdef...",
        "amount": "50000"
      }
    ],
    "state_hash": "0xLedgerStateHash",
    "server_signature": {
      "v": "27",
      "r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
      "s": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
    }
  }], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

### Resize Channel

Adjusts the capacity of a channel.

**Request:**

```json
{
  "req": [6, "resize_channel", [{
    "channel_id": "0x4567890123abcdef...",
    "participant_change": "50000",
    "funds_destination": "0x1234567890abcdef..."
  }], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [6, "resize_channel", [{
    "channel_id": "0x4567890123abcdef...",
    "intent": 2,
    "version": "124",
    "state_data": "0x0000000000000000000000000000000000000000000000000000000000002ec7",
    "allocations": [
      {
        "destination": "0x1234567890abcdef...",
        "token": "0xeeee567890abcdef...",
        "amount": "100000"
      },
      {
        "destination": "0xbbbb567890abcdef...", // Broker address
        "token": "0xeeee567890abcdef...",
        "amount": "0"
      }
    ],
    "state_hash": "0xLedgerStateHash",
    "server_signature": {
      "v": "28",
      "r": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
      "s": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
    }
  }], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

## Messaging

### Send Message in Virtual Application

Sends a message to all participants in a virtual application.

**Request:**

```json
{
  "req": [6, "message", [{
    "message": "Hello, application participants!"
  }], 1619123456789],
  "acc": "0x3456789012abcdef...", // Virtual application ID
  "sig": ["0x9876fedcba..."]
}
```

### Send Response in Virtual Application

Responses can also be forwarded to all participants in a virtual application by including the AppID field:

```json
{
  "res": [6, "message", [{
    "message": "I confirm that I have received your message!"
  }], 1619123456789],
  "acc": "0x3456789012abcdef...", // Virtual application ID
  "sig": ["0x9876fedcba..."]
}
```

## Utility Methods

### Ping

Simple ping to check connectivity.

**Request:**

```json
{
  "req": [7, "ping", [], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [7, "pong", [], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

### Get Configuration

Retrieves broker configuration information.

**Request:**

```json
{
  "req": [8, "get_config", [], 1619123456789],
  "sig": ["0x9876fedcba..."]
}
```

**Response:**

```json
{
  "res": [8, "get_config", [{
    "brokerAddress": "0xbbbb567890abcdef..."
  }], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```

## Error Handling

When an error occurs, the server responds with an error message:

```json
{
  "res": [REQUEST_ID, "error", [{
    "error": "Error message describing what went wrong"
  }], 1619123456789],
  "sig": ["0xabcd1234..."]
}
```
