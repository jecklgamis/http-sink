// http-sink is a simple request sink: it accepts any method or subpath under
// /sink, echoes it back, simulates latency and failures per request (or from a
// static rules file), and shows live traffic stats on a dashboard. It keeps no
// state that needs sharing, so any number of replicas can run side by side.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jecklgamis/http-sink/internal/buildinfo"
	"github.com/jecklgamis/http-sink/internal/server"
	"github.com/jecklgamis/http-sink/internal/statsd"
)

const (
	certFile   = "server.crt"
	keyFile    = "server.key"
	statsdAddr = "localhost:8125"
)

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	statsdClient, err := statsd.New(statsdAddr)
	if err != nil {
		log.Printf("statsd disabled: %v", err)
	}

	opts := server.Options{
		Statsd:        statsdClient,
		AccessLog:     os.Stdout,
		AccessLogJSON: cfg.LogJSON,
		BuildInfo:     buildinfo.Data,
		Development:   cfg.Development,
		MaxDelay:      cfg.MaxDelay,
		MaxBodyBytes:  cfg.MaxBodyBytes,
		NoRateLimit:   cfg.RateLimit == 0,
	}
	if cfg.RateLimit > 0 {
		opts.RateLimiter = newRateLimiter(cfg.RateLimit)
	}
	app := server.New(opts)

	if cfg.RulesFile != "" {
		rules, err := server.LoadRulesFile(cfg.RulesFile)
		if err != nil {
			log.Fatalf("rules: %v", err)
		}
		if err := app.SetRules(rules); err != nil {
			log.Fatalf("rules: %s: %v", cfg.RulesFile, err)
		}
		log.Printf("loaded %d rule(s) from %s", len(rules), cfg.RulesFile)
	}

	servers := []*http.Server{newServer(":"+cfg.Port, app)}
	var tlsServer *http.Server
	if fileExists(certFile) && fileExists(keyFile) {
		tlsServer = newServer(":"+cfg.HTTPSPort, app)
		servers = append(servers, tlsServer)
	} else {
		log.Printf("%s/%s not found; HTTPS disabled (run ./generate-ssl-certs.sh)", certFile, keyFile)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	errc := make(chan error, len(servers))
	for _, srv := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("App ready on port %s", srv.Addr[1:])
			var err error
			if srv == tlsServer {
				err = srv.ListenAndServeTLS(certFile, keyFile)
			} else {
				err = srv.ListenAndServe()
			}
			if !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}

	failed := false
	select {
	case <-ctx.Done():
		log.Println("shutting down")
	case err := <-errc:
		log.Printf("server error: %v", err)
		failed = true
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(shutdownCtx)
	}
	wg.Wait()
	_ = statsdClient.Close()
	if failed {
		os.Exit(1)
	}
}

func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}
