// http-sink is a simple request sink: it accepts any method or subpath under
// /sink, echoes it back, simulates latency and failures, and shows live
// traffic stats on a dashboard.
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
	defaultHTTPPort = "38080"
	defaultDataDir  = "data"
	httpsAddr       = ":8443"
	certFile        = "server.crt"
	keyFile         = "server.key"
	statsdAddr      = "localhost:8125"
)

func main() {
	statsdClient, err := statsd.New(statsdAddr)
	if err != nil {
		log.Printf("statsd disabled: %v", err)
	}

	app := server.New(server.Options{
		Statsd:      statsdClient,
		AccessLog:   os.Stdout,
		BuildInfo:   buildinfo.Data,
		Development: os.Getenv("APP_ENV") == "development",
	})

	// SINK_DATA_DIR holds the persisted latency/failure configs. Unset uses
	// ./data; set it to an empty string to disable persistence.
	dataDir, set := os.LookupEnv("SINK_DATA_DIR")
	if !set {
		dataDir = defaultDataDir
	}
	if dataDir != "" {
		if err := app.EnablePersistence(dataDir); err != nil {
			log.Fatalf("persistence: %v", err)
		}
		log.Printf("persisting latency/failure configs in %s", dataDir)
	} else {
		log.Printf("persistence disabled (SINK_DATA_DIR is empty)")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultHTTPPort
	}

	servers := []*http.Server{newServer(":"+port, app)}
	var tlsServer *http.Server
	if fileExists(certFile) && fileExists(keyFile) {
		tlsServer = newServer(httpsAddr, app)
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
	_ = app.Close() // flush persisted configs
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
