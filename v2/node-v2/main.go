package main

var ledger *Ledger
var channelRepo *ChannelRepo

func main() {
	ledger = NewLedger()
	channelRepo = NewChannelRepo()
}
