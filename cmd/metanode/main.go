package main

import (
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"

	"dfsha/internal/metanode/https"

	_ "modernc.org/sqlite"
)

func main() {
	debug := flag.Bool("debug", false, "Run HTTP without TLS")
	production := flag.Bool("production", false, "Run HTTPS with TLS")
	addr := flag.String("addr", "", "Server address")
	dbPath := flag.String("db", "", "SQLite database path")
	certFile := flag.String("cert", "", "TLS certificate file")
	keyFile := flag.String("key", "", "TLS private key file")

	flag.Parse()

	if *debug == *production {
		log.Fatal("Specify exactly one of --debug or --production")
	}

	if *addr == "" || *dbPath == "" {
		log.Fatal("--addr and --db are required")
	}

	if *production && (*certFile == "" || *keyFile == "") {
		log.Fatal("--cert and --key are required in production")
	}

	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		log.Fatal(err)
	}

	server := https.NewServer(*addr, db)

	if *debug {
		log.Printf("DEBUG: HTTP listening on %s", *addr)
		err = server.ListenAndServe()
	} else {
		log.Printf("PRODUCTION: HTTPS listening on %s", *addr)
		err = server.ListenAndServeTLS(*certFile, *keyFile)
	}

	if err != nil && err != http.ErrServerClosed {
		log.Printf("server error: %v", err)
		os.Exit(1)
	}
}
