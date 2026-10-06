package main

import "github.com/conn-castle/agent-layer/internal/messages"

const (
	commandInit    = messages.InitUse
	commandUpdate  = messages.UpdateUse
	commandUpgrade = messages.UpgradeUse
	unknownVersion = "unknown"
	noSyncFlag     = "--no-sync"
	noSyncPrefix   = "--no-sync="

	flagQuiet       = "--quiet"
	flagQuietShort  = "-q"
	flagQuietPrefix = "--quiet="
)
