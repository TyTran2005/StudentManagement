package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"student-management-api/internal/config"
	"student-management-api/internal/database"
	"student-management-api/internal/graphql"
	"student-management-api/internal/handlers"
	"student-management-api/internal/middleware"
	"student-management-api/internal/resolvers"

	gqlhandler "github.com/graphql-go/handler"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("FATAL: Load config: %v", err)
	}
	db, err := database.ConnectDatabase(cfg)
	if err != nil {
		log.Fatalf("FATAL: Connect DB: %v", err)
	}
	resolvers.DB = db
	log.Println("INFO: Database connection assigned.")
	log.Println("INFO: Applying database migrations...")
	dbURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBSslmode)
	migrationPath := "file://database/migrations"
	m, err := migrate.New(migrationPath, dbURL)
	if err != nil {
		log.Fatalf("FATAL: Init migrate: %v", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("FATAL: Apply migrations: %v", err)
	} else if errors.Is(err, migrate.ErrNoChange) {
		log.Println("INFO: No new migrations.")
	} else {
		log.Println("INFO: Migrations applied.")
	}
	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		log.Printf("WARN: Get migration version: %v", err)
	} else if errors.Is(err, migrate.ErrNilVersion) {
		log.Println("INFO: No migrations applied yet.")
	} else {
		log.Printf("INFO: Migration version: %d, Dirty: %v", version, dirty)
		if dirty {
			log.Println("WARNING: Migration dirty.")
		}
	}

	gqlHandler := gqlhandler.New(&gqlhandler.Config{
		Schema:     &graphql.Schema,
		Pretty:     true,
		GraphiQL:   false,
		Playground: false,
	})

	mux := http.NewServeMux()

	mux.Handle("/graphql", middleware.AuthMiddleware(gqlHandler))

	mux.HandleFunc("/auth/webhook", handlers.AuthWebhookHandler(db))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		sqlDB, err := db.DB()
		if err != nil {
			http.Error(w, "Failed DB conn", http.StatusInternalServerError)
			return
		}
		if err := sqlDB.Ping(); err != nil {
			http.Error(w, "DB ping failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	serverAddr := ":" + cfg.ServerPort
	log.Printf("INFO: Starting server on %s", serverAddr)
	log.Printf("INFO: Custom GraphQL endpoint (for Hasura Remote Schema): http://localhost%s/graphql", serverAddr)
	log.Printf("INFO: Hasura Auth Webhook endpoint: POST /auth/webhook")
	log.Printf("INFO: Health check endpoint: GET /health")

	if err := http.ListenAndServe(serverAddr, mux); err != nil {
		log.Fatalf("FATAL: Server failed to start: %v", err)
	}
}
