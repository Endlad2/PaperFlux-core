package tunnel

import "testing"

func TestExitModeParsing(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  ExitMode
	}{
		{"", ExitModeProxy},
		{"proxy", ExitModeProxy},
		{"raw", ExitModeRaw},
	} {
		mode, err := ParseExitMode(tc.input)
		if err != nil || mode != tc.want {
			t.Fatalf("ParseExitMode(%q) = %s, %v; want %s", tc.input, mode, err, tc.want)
		}
	}
	if _, err := ParseExitMode("invalid"); err == nil {
		t.Fatal("unknown mode must be rejected")
	}
}
