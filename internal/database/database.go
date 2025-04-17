package database

import (
	"fmt"
	"log"
	"student-management-api/internal/config"
	"student-management-api/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func ConnectDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=Asia/Shanghai",
		cfg.DBHost,
		cfg.DBUser,
		cfg.DBPassword,
		cfg.DBName,
		cfg.DBPort,
		cfg.DBSslmode,
	)

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		log.Printf("Failed to connect to database: %v", err)
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	log.Println("Database connection established successfully.")

	log.Println("Running database migrations...")
	err = DB.AutoMigrate(
		&models.User{},
		&models.Class{},
		&models.StudentClass{},
	)
	if err != nil {
		log.Printf("Failed to auto migrate database: %v", err)
		return nil, fmt.Errorf("failed to auto migrate database: %w", err)
	}
	log.Println("Database migrations completed.")

	return DB, nil
}
