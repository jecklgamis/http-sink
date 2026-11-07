package main

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfigDefaults(t *testing.T) {
	c, err := loadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "38080" || c.HTTPSPort != "8443" || c.MaxDelay != 10*time.Second || c.MaxBodyBytes != 100*1024 ||
		c.RateLimit != 1000 || c.LogJSON || c.Development || c.RulesFile != "" {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	c, err := loadConfig(env(map[string]string{
		"PORT": "9000", "HTTPS_PORT": "9443", "MAX_DURATION": "30s", "MAX_BODY_SIZE": "2048", "RATE_LIMIT": "0",
		"LOG_FORMAT": "json", "SINK_RULES_FILE": "/etc/sink/rules.json", "APP_ENV": "development",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "9000" || c.HTTPSPort != "9443" || c.MaxDelay != 30*time.Second || c.MaxBodyBytes != 2048 ||
		c.RateLimit != 0 || !c.LogJSON || !c.Development || c.RulesFile != "/etc/sink/rules.json" {
		t.Fatalf("overrides = %+v", c)
	}
}

func TestLoadConfigRejectsBadValues(t *testing.T) {
	for k, v := range map[string]string{
		"MAX_DURATION": "forever", "MAX_BODY_SIZE": "-1", "RATE_LIMIT": "-5", "LOG_FORMAT": "xml",
	} {
		if _, err := loadConfig(env(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q should be rejected", k, v)
		}
	}
	for k, v := range map[string]string{"MAX_DURATION": "0s", "MAX_BODY_SIZE": "0", "RATE_LIMIT": "abc"} {
		if _, err := loadConfig(env(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q should be rejected", k, v)
		}
	}
}
