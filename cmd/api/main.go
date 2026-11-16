package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"
	"github.com/waste-not-app/waste-not-backend/internal/api"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/database"
)

// @title WasteNot API
// @version 1.0
// @description API Documentation for WasteNot Android Backend (Waste Logging, Gamification, Guides & AI Scan)
// @host localhost:8080
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("API stopped; check configuration and database availability", "detail", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		log.Error(err.Error())
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	dbPool, err := database.Open(startup, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer dbPool.Close()

	srv, err := api.New(dbPool, cfg, log)
	if err != nil {
		return err
	}
	defer srv.Close()

	result := make(chan error, 1)
	go func() {
		log.Info("API listening", "address", cfg.Address)
		result <- srv.App.Listen(cfg.Address, fiber.ListenConfig{
			DisableStartupMessage: true,
		})
	}()

	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			return srv.App.ShutdownWithContext(shutdown)
		case err := <-result:
			return err
		case <-tick.C:
			cleanup, cancel := context.WithTimeout(ctx, 10*time.Second)
			if database.Cleanup(cleanup, dbPool) != nil {
				log.Error("cleanup failed")
			}
			cancel()
		}
	}
}
