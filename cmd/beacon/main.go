// beacon is a self-hosted tailnet status dashboard: one fast page that makes
// a dead backup or broken sync impossible to miss.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"beacon/internal/config"
	"beacon/internal/provider"
	_ "beacon/internal/providers" // register built-in provider types
	"beacon/internal/server"
)

func main() {
	configPath := flag.String("config", "", "path to TOML config file")
	demo := flag.Bool("demo", false, "serve fabricated data for every provider type")
	listen := flag.String("listen", "", "override listen address")
	flag.Parse()
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var cfg config.File
	var src server.Source
	if *demo {
		cfg, src = demoConfig(), demoSource{}
		log.Print("beacon: --demo mode, serving fabricated data")
	} else {
		if *configPath == "" {
			log.Fatal("beacon: -config is required (or use -demo)")
		}
		var err error
		if cfg, err = config.Load(*configPath); err != nil {
			log.Fatalf("beacon: %v", err)
		}
		runner, err := provider.NewRunner(cfg.Providers, cfg.StaleAfter)
		if err != nil {
			log.Fatalf("beacon: %v", err)
		}
		runner.Start(ctx)
		src = runner
	}
	if *listen != "" {
		cfg.Listen = *listen
	}

	srv := &http.Server{Addr: cfg.Listen, Handler: server.New(src, cfg)}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Printf("beacon: listening on http://%s", cfg.Listen)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("beacon: %v", err)
	}
}
