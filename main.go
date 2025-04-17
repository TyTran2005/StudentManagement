package main

import (
	"log"
	"net/http"
	"student-management-api/internal/config"
	"student-management-api/internal/database"
	"student-management-api/internal/graphql"
	"student-management-api/internal/middleware"
	"student-management-api/internal/resolvers"

	gqlhandler "github.com/graphql-go/handler"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("FATAL: Failed to load config: %v", err)
	}

	db, err := database.ConnectDatabase(cfg)
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to database or run migrations: %v", err)
	}

	resolvers.DB = db
	log.Println("INFO: Database connection assigned to resolvers.")

	gqlHandler := gqlhandler.New(&gqlhandler.Config{
		Schema:     &graphql.Schema,
		Pretty:     true,
		GraphiQL:   true,
		Playground: false,
	})

	mux := http.NewServeMux()

	mux.Handle("/graphql", middleware.AuthMiddleware(gqlHandler))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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
