package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/waste-not-app/waste-not-backend/internal/api"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/database"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if run(log) != nil {
		log.Error("API stopped; check configuration and database availability")
		os.Exit(1)
	}
}
func run(log *slog.Logger) error {
	c, e := config.Load()
	if e != nil {
		log.Error(e.Error())
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, e := database.Open(startup, c.DatabaseURL)
	if e != nil {
		return e
	}
	defer db.Close()
	app, e := api.New(db, c, log)
	if e != nil {
		return e
	}
	server := &http.Server{Addr: c.Address, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	result := make(chan error, 1)
	go func() { log.Info("API listening", "address", c.Address); result <- server.ListenAndServe() }()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		case e := <-result:
			if e == http.ErrServerClosed {
				return nil
			}
			return e
		case <-tick.C:
			cleanup, cancel := context.WithTimeout(ctx, 10*time.Second)
			if database.Cleanup(cleanup, db) != nil {
				log.Error("cleanup failed")
			}
			cancel()
		}
	}
}
