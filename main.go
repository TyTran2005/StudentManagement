package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"student-management-api/internal/config"
	"student-management-api/internal/database"
	"student-management-api/internal/graphql"
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
		log.Fatalf("FATAL: Failed to load config: %v", err)
	}

	db, err := database.ConnectDatabase(cfg)
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to database: %v", err)
	}
	resolvers.DB = db
	log.Println("INFO: Database connection assigned to resolvers.")

	log.Println("INFO: Applying database migrations...")

	dbURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBSslmode)

	migrationPath := "file://database/migrations"

	m, err := migrate.New(migrationPath, dbURL)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize migrate instance: %v", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("FATAL: Failed to apply migrations: %v", err)
	} else if errors.Is(err, migrate.ErrNoChange) {
		log.Println("INFO: No new migrations to apply.")
	} else {
		log.Println("INFO: Database migrations applied successfully.")
	}

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		log.Printf("WARNING: Could not get migration version: %v", err)
	} else if errors.Is(err, migrate.ErrNilVersion) {
		log.Println("INFO: No migrations applied yet (version nil).")
	} else {
		log.Printf("INFO: Current migration version: %d, Dirty: %v", version, dirty)
		if dirty {
			log.Println("WARNING: Migration is dirty. Check the schema_migrations table.")
		}
	}
	gqlHandler := gqlhandler.New(&gqlhandler.Config{
		Schema:     &graphql.Schema,
		Pretty:     true,
		GraphiQL:   true,
		Playground: false,
	})

	mux := http.NewServeMux()
	mux.Handle("/graphql", middleware.AuthMiddleware(gqlHandler))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		sqlDB, err := db.DB()
		if err != nil {
			http.Error(w, "Failed to get underlying DB connection", http.StatusInternalServerError)
			return
		}
		if err := sqlDB.Ping(); err != nil {
			http.Error(w, "Database connection failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	serverAddr := ":" + cfg.ServerPort
	log.Printf("INFO: Starting GraphQL server on %s", serverAddr)
	log.Printf("INFO: GraphiQL UI available at http://localhost%s/graphql", serverAddr)

	if err := http.ListenAndServe(serverAddr, mux); err != nil {
		log.Fatalf("FATAL: Server failed to start: %v", err)
	}
}
