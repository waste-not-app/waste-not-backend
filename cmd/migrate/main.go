package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/database"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, e := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		log.Fatal("database unavailable")
	}
	defer db.Close()
	if e = database.Migrate(ctx, db); e != nil {
		log.Fatal(e)
	}
	log.Print("migrations applied")
}
