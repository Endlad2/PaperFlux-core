package main

import (
	"flag"
	"io"
	"testing"

	"universal-bypass-tool/tunnel"
)

func TestExitModeCLIDefaultAndExplicitRaw(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want tunnel.ExitMode
	}{
		{"omitted", nil, tunnel.ExitModeProxy},
		{"explicit-proxy", []string{"--mode", "proxy"}, tunnel.ExitModeProxy},
		{"explicit-raw", []string{"--mode", "raw"}, tunnel.ExitModeRaw},
		{"empty-uses-default", []string{"--mode", ""}, tunnel.ExitModeProxy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("exit-mode-test", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			value := registerExitModeFlag(flags)
			if flags.Lookup("mode").DefValue != "proxy" {
				t.Fatal("CLI/help must advertise proxy by default")
			}
			if err := flags.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			got, err := tunnel.ParseExitMode(*value)
			if err != nil || got != tc.want {
				t.Fatalf("got mode=%s error=%v, want %s", got, err, tc.want)
			}
		})
	}
}
