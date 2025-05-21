#!/bin/bash

# Export or replace "localhost:8000" with path to your clearnode server
SERVER=${SERVER:-"ws://localhost:8000/ws"}

echo "===================================="
echo "Clearnode RPC Testing Tool"
echo "This Testing Tool may not be sufficient to cover all use cases."
echo "Use go CLI with custom parameters for more advanced testing."
echo "===================================="
echo "Using server: $SERVER"
echo ""

function show_menu {
  echo "Choose a test to run:"
  echo "1) Generate new private key"
  echo "2) Ping test (check connectivity)"
  echo "3) Get server configuration"
  echo "4) Get account balances"
  echo "5) Get ledger entries"
  echo "6) Get all channels"
  echo "7) Get all supported assets"
  echo "8) Get assets for Polygon (chain_id: 137)"
  echo "9) Get RPC history"
  echo "10) Get app definition (requires app_session_id)"
  echo "11) Create app session (virtual app)"
  echo "12) Close app session (virtual app)"
  echo "13) Resize channel"
  echo "14) Close channel"
  echo "15) Custom command"
  echo "0) Exit"
}

function run_test {
  case $1 in
    1)
      echo "Generating new private key..."
      go run . --genkey
      ;;
    2)
      echo "Running ping test..."
      go run . --method ping --send --server "$SERVER"
      ;;
    3)
      echo "Getting server configuration..."
      go run . --method get_config --send --server "$SERVER"
      ;;
    4)
      echo "Getting account balances..."
      go run . --method get_ledger_balances --send --server "$SERVER"
      ;;
    5)
      echo "Getting ledger entries..."
      go run . --method get_ledger_entries --send --server "$SERVER"
      ;;
    6)
      echo "Getting all channels..."
      go run . --method get_channels --send --server "$SERVER"
      ;;
    7)
      echo "Getting all supported assets..."
      go run . --method get_assets --send --server "$SERVER"
      ;;
    8)
      echo "Getting assets for Polygon (chain_id: 137)..."
      go run . --method get_assets --params '[{"chain_id":137}]' --send --server "$SERVER"
      ;;
    9)
      echo "Getting RPC history..."
      go run . --method get_rpc_history --send --server "$SERVER"
      ;;
    10)
      echo "Enter app session ID (format: 0x...):"
      read app_id
      echo "Getting app definition for $app_id..."
      go run . --method get_app_definition --params "[{\"app_session_id\":\"$app_id\"}]" --send --server "$SERVER"
      ;;
    11)
      echo "Creating a new app session (virtual app)..."
      
      # Ask for number of participants
      echo "Enter the number of participants:"
      read num_participants
      
      # Initialize arrays
      participants=()
      weights=()
      
      # Get all participants
      for (( i=1; i<=num_participants; i++ ))
      do
        echo "Enter participant $i address:"
        read participant
        participants+=("$participant")
        
        echo "Enter participant $i voting weight:"
        read weight
        weights+=($weight)
      done
      
      # Ask for quorum
      echo "Enter quorum percentage (e.g., 100 means all participants must agree):"
      read quorum
      
      # Ask for asset
      echo "Enter asset symbol (e.g., usdc):"
      read asset
      
      # Build JSON strings
      participants_json=$(printf '"%s",' "${participants[@]}")
      participants_json="[${participants_json%,}]"
      
      weights_json=$(printf '%s,' "${weights[@]}")
      weights_json="[${weights_json%,}]"
      
      # Get allocation amounts and build allocations JSON
      allocations=""
      for (( i=0; i<num_participants; i++ ))
      do
        echo "Enter allocation amount for ${participants[$i]}:"
        read amount
        
        if [ $i -gt 0 ]; then
          allocations+=","
        fi
        
        allocations+="{\"participant\":\"${participants[$i]}\",\"asset\":\"$asset\",\"amount\":\"$amount\"}"
      done
      
      allocations="[$allocations]"
      
      echo "Creating app session with $num_participants participants"
      go run . --method create_app_session --params "[{\"definition\":{\"protocol\":\"NitroRPC/0.2\",\"participants\":$participants_json,\"weights\":$weights_json,\"quorum\":$quorum,\"challenge\":86400,\"nonce\":1},\"allocations\":$allocations}]" --send --server "$SERVER"
      ;;
    12)
      echo "Closing an app session (virtual app)..."
      
      # Ask for app session ID
      echo "Enter app session ID to close (format: 0x...):"
      read app_id
      
      # For now, ask the user how many participants there are
      echo "Enter the number of participants in this app session:"
      read num_participants
      
      # Ask for asset
      echo "Enter asset symbol (e.g., usdc):"
      read asset
      
      # Get allocation amounts and build allocations JSON
      allocations=""
      for (( i=1; i<=num_participants; i++ ))
      do
        echo "Enter participant $i address:"
        read participant
        
        echo "Enter final allocation amount for participant $i:"
        read amount
        
        if [ $i -gt 1 ]; then
          allocations+=","
        fi
        
        allocations+="{\"participant\":\"$participant\",\"asset\":\"$asset\",\"amount\":\"$amount\"}"
      done
      
      allocations="[$allocations]"
      
      echo "Closing app session $app_id with $num_participants participants"
      go run . --method close_app_session --params "[{\"app_session_id\":\"$app_id\",\"allocations\":$allocations}]" --send --server "$SERVER"
      ;;
    13)
      echo "Resizing a channel..."
      echo "Enter channel ID to resize (format: 0x...):"
      read channel_id
      
      echo "Enter the resize amount (use negative for decrease, positive for increase):"
      read resize_amount
      
      echo "Enter the allocate amount (usually 0):"
      read allocate_amount
      
      echo "Enter funds destination address (usually your address):"
      read funds_destination
      
      echo "Resizing channel $channel_id"
      go run . --method resize_channel --params "[{\"channel_id\":\"$channel_id\",\"resize_amount\":\"$resize_amount\",\"allocate_amount\":\"$allocate_amount\",\"funds_destination\":\"$funds_destination\"}]" --send --server "$SERVER"
      ;;
    14)
      echo "Closing a channel..."
      echo "Enter channel ID to close (format: 0x...):"
      read channel_id
      
      echo "Enter funds destination address (usually your address):"
      read funds_destination
      
      echo "Closing channel $channel_id"
      go run . --method close_channel --params "[{\"channel_id\":\"$channel_id\",\"funds_destination\":\"$funds_destination\"}]" --send --server "$SERVER"
      ;;
    15)
      echo "Enter method name:"
      read method
      echo "Enter params as JSON array (leave empty for none):"
      read params
      if [ -z "$params" ]; then
        params="[]"
      fi
      echo "Running custom command: $method with params $params"
      go run . --method "$method" --params "$params" --send --server "$SERVER"
      ;;
    0)
      echo "Exiting..."
      exit 0
      ;;
    *)
      echo "Invalid option"
      ;;
  esac
  
  echo ""
  echo "Press Enter to continue..."
  read
}

while true; do
  clear
  show_menu
  echo ""
  echo -n "Enter your choice: "
  read choice
  echo ""
  run_test $choice
done
