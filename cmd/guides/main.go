package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/database"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: guides reviewed-guides.json")
	}
	file, e := os.Open(os.Args[1])
	if e != nil {
		log.Fatal("cannot open guide file")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 5*1024*1024))
	decoder.DisallowUnknownFields()
	var guides []database.Guide
	if e = decoder.Decode(&guides); e != nil {
		log.Fatal("invalid guide JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		log.Fatal("trailing guide JSON")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, e := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		log.Fatal("database unavailable")
	}
	defer db.Close()
	if e = database.ImportGuides(ctx, db, guides); e != nil {
		log.Fatal(e)
	}
	log.Printf("imported %d reviewed guides", len(guides))
}
