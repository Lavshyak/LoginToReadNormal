// Command app runs the LoginToReadNormal private paste service.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"logintoreadnormal/internal/config"
	"logintoreadnormal/internal/db"
	"logintoreadnormal/internal/repo"
	"logintoreadnormal/internal/server"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {

	log.Printf("version 1.0.1")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	d, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()

	if err := db.Migrate(ctx, d); err != nil {
		return err
	}
	created, err := db.SeedAdmin(ctx, d, cfg.AdminLogin, cfg.AdminPrimaryPass)
	if err != nil {
		return err
	}
	if created {
		log.Printf("seeded administrator %q with an active primary login", cfg.AdminLogin)
	} else {
		log.Printf("administrator %q already exists; seed skipped", cfg.AdminLogin)
	}

	srv, err := server.New(ctx, cfg, repo.New(d))
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("listening on %s (primary path %q, root url %q)", cfg.ListenAddr, cfg.PrimaryPath, cfg.RootURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}
