package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/jecklgamis/http-sink/internal/ratelimit"
	"github.com/jecklgamis/http-sink/internal/server"
)

func newRateLimiter(perSecond int) *ratelimit.Limiter { return ratelimit.New(perSecond, time.Second) }

// config is the process configuration, read from environment variables.
// The app keeps no state of its own, so every replica started with the same
// environment (and rules file) behaves identically.
type config struct {
	Port         string        // PORT, HTTP port (default 38080)
	HTTPSPort    string        // HTTPS_PORT, HTTPS port when server.crt/server.key exist (default 8443)
	RulesFile    string        // SINK_RULES_FILE, optional static per-path rules
	MaxDelay     time.Duration // MAX_DURATION, longest injectable delay (default 10s)
	MaxBodyBytes int64         // MAX_BODY_SIZE, largest parsed request body in bytes (default 102400)
	RateLimit    int           // RATE_LIMIT, global requests/second; 0 disables (default 1000)
	LogJSON      bool          // LOG_FORMAT=json for JSON access logs (default text/combined)
	Development  bool          // APP_ENV=development adds error details to error pages
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		Port:         "38080",
		HTTPSPort:    "8443",
		MaxDelay:     server.DefaultMaxDelay,
		MaxBodyBytes: server.DefaultMaxBodyBytes,
		RateLimit:    server.DefaultRateLimit,
	}
	if v := getenv("PORT"); v != "" {
		c.Port = v
	}
	if v := getenv("HTTPS_PORT"); v != "" {
		c.HTTPSPort = v
	}
	c.RulesFile = getenv("SINK_RULES_FILE")
	c.Development = getenv("APP_ENV") == "development"

	if v := getenv("MAX_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return c, fmt.Errorf("MAX_DURATION=%q: must be a positive duration like 10s", v)
		}
		c.MaxDelay = d
	}
	if v := getenv("MAX_BODY_SIZE"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return c, fmt.Errorf("MAX_BODY_SIZE=%q: must be a positive number of bytes", v)
		}
		c.MaxBodyBytes = n
	}
	if v := getenv("RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return c, fmt.Errorf("RATE_LIMIT=%q: must be a requests-per-second number, or 0 to disable", v)
		}
		c.RateLimit = n
	}
	switch v := getenv("LOG_FORMAT"); v {
	case "", "text":
	case "json":
		c.LogJSON = true
	default:
		return c, fmt.Errorf("LOG_FORMAT=%q: must be text or json", v)
	}
	return c, nil
}
