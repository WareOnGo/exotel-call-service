package config

import (
	"testing"
	"time"
)

func TestR2EndpointURL(t *testing.T) {
	cases := []struct {
		name           string
		endpoint, acct string
		want           string
	}{
		{"explicit endpoint wins", "https://custom.example.com", "acct123", "https://custom.example.com"},
		{"derived from account id", "", "acct123", "https://acct123.r2.cloudflarestorage.com"},
		{"neither set", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{R2Endpoint: c.endpoint, R2AccountID: c.acct}
			if got := cfg.R2EndpointURL(); got != c.want {
				t.Errorf("R2EndpointURL() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestR2Configured(t *testing.T) {
	full := Config{
		R2AccountID: "a", R2AccessKeyID: "k", R2SecretAccessKey: "s", R2Bucket: "b",
	}
	if !full.R2Configured() {
		t.Error("expected configured when all set")
	}
	// Each missing piece disables it.
	for _, mut := range []func(*Config){
		func(c *Config) { c.R2AccountID = ""; c.R2Endpoint = "" },
		func(c *Config) { c.R2AccessKeyID = "" },
		func(c *Config) { c.R2SecretAccessKey = "" },
		func(c *Config) { c.R2Bucket = "" },
	} {
		c := full
		mut(&c)
		if c.R2Configured() {
			t.Errorf("expected NOT configured after mutation, cfg=%+v", c)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	// Required-ish vars left unset; we only assert the typed defaults.
	t.Setenv("EXOTEL_SUBDOMAIN", "") // force default branch
	cfg := Load()
	if cfg.Port != "8080" {
		t.Errorf("Port default = %q, want 8080", cfg.Port)
	}
	if cfg.ExotelSubdomain != "api.exotel.com" {
		t.Errorf("ExotelSubdomain default = %q", cfg.ExotelSubdomain)
	}
	if cfg.RouterDBTimeout != 150*time.Millisecond {
		t.Errorf("RouterDBTimeout default = %v", cfg.RouterDBTimeout)
	}
	if cfg.ArchiveInterval != 24*time.Hour {
		t.Errorf("ArchiveInterval default = %v", cfg.ArchiveInterval)
	}
	if cfg.R2KeyPrefix != "recordings" {
		t.Errorf("R2KeyPrefix default = %q", cfg.R2KeyPrefix)
	}
}
