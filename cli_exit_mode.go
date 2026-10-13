package main

import (
	"flag"

	"universal-bypass-tool/tunnel"
)

func registerExitModeFlag(flags *flag.FlagSet) *string {
	return flags.String("mode", tunnel.DefaultExitMode.String(), "Exit-node mode: proxy (default) or raw (explicit opt-in)")
}
