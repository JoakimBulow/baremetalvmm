package main

import "testing"

func TestParseOnOff(t *testing.T) {
	for in, want := range map[string]bool{"on": true, "true": true, "yes": true, "1": true, "off": false, "false": false, "no": false, "0": false} {
		got, err := parseOnOff(in)
		if err != nil || got != want {
			t.Errorf("parseOnOff(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseOnOff("maybe"); err == nil {
		t.Error("parseOnOff(\"maybe\") should fail")
	}
}
