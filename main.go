package main

import (
	"log"
	"net/http"
	"student-management-api/internal/config"
	"student-management-api/internal/graphql"
	"student-management-api/internal/models"
	"student-management-api/internal/resolvers"

	"github.com/graphql-go/handler"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	dsn := "host=" + cfg.DBHost +
		" port=" + cfg.DBPort +
		" user=" + cfg.DBUser +
		" password=" + cfg.DBPassword +
		" dbname=" + cfg.DBName +
		" sslmode=" + cfg.DBSslmode
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("Connected to the database successfully!")

	resolvers.DB = db

	err = db.AutoMigrate(&models.User{}, &models.Class{}, &models.StudentClass{})
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	h := handler.New(&handler.Config{
		Schema:   &graphql.Schema,
		Pretty:   true,
		GraphiQL: true,
	})

	http.Handle("/graphql", h)

	log.Println("Server is running on port", cfg.ServerPort)
	log.Fatal(http.ListenAndServe(":"+cfg.ServerPort, nil))
}
