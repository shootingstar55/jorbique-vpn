package config

import (
	"strings"
	"testing"
)

func TestParseAndValidateDirectDomains_OK(t *testing.T) {
	got, err := ParseAndValidateDirectDomains("example.com\n*.cdn.example.org\n# comment\n\nEXAMPLE.COM\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 unique domains, got %d: %v", len(got), got)
	}
	if got[0] != "example.com" || got[1] != "cdn.example.org" {
		t.Fatalf("unexpected order/values: %v", got)
	}
}

func TestParseAndValidateDirectDomains_Errors(t *testing.T) {
	_, err := ParseAndValidateDirectDomains("bad..host")
	if err == nil {
		t.Fatal("expected error for ..")
	}
	_, err = ParseAndValidateDirectDomains("has space.com")
	if err == nil {
		t.Fatal("expected error for space")
	}
}

func TestParseAndValidateDirectDomains_Empty(t *testing.T) {
	got, err := ParseAndValidateDirectDomains("   \n# only comment\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestParseAndValidateDirectDomains_LongLabel(t *testing.T) {
	long := strings.Repeat("a", 64) + ".com"
	_, err := ParseAndValidateDirectDomains(long)
	if err == nil {
		t.Fatal("expected error for label > 63")
	}
}
