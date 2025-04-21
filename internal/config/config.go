package config

import (
	"errors"
	"fmt"
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	DBHost     string `mapstructure:"DB_HOST"`
	DBPort     string `mapstructure:"DB_PORT"`
	DBUser     string `mapstructure:"DB_USER"`
	DBPassword string `mapstructure:"DB_PASSWORD"`
	DBName     string `mapstructure:"DB_NAME"`
	DBSslmode  string `mapstructure:"DB_SSLMODE"`
	ServerPort string `mapstructure:"SERVER_PORT"`

	HasuraJWTType string `mapstructure:"HASURA_JWT_TYPE"`
	HasuraJWTKey  string `mapstructure:"HASURA_JWT_KEY"`
	HasuraJWKURL  string `mapstructure:"HASURA_JWK_URL"`
}

var AppConfig *Config

func LoadConfig() (*Config, error) {
	viper.AddConfigPath(".")
	viper.SetConfigName(".env")
	viper.SetConfigType("env")

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Println("INFO: .env file not found, relying on environment variables.")
		} else {
			log.Printf("WARN: Error reading config file: %v. Relying on environment variables.", err)
		}
	}

	viper.SetDefault("SERVER_PORT", "8080")
	viper.SetDefault("HASURA_JWT_TYPE", "HS256")

	var cfg Config
	err := viper.Unmarshal(&cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to decode config: %w", err)
	}

	if cfg.DBUser == "" || cfg.DBPassword == "" || cfg.DBName == "" {
		log.Println("WARNING: Critical database configuration (DB_USER, DB_PASSWORD, DB_NAME) is missing.")
	}

	switch cfg.HasuraJWTType {
	case "HS256":
		if cfg.HasuraJWTKey == "" {
			log.Println("ERROR: HASURA_JWT_TYPE is HS256 but HASURA_JWT_KEY is missing.")
			return nil, errors.New("missing required configuration: HASURA_JWT_KEY for HS256")
		}
	case "RS256":
		if cfg.HasuraJWKURL == "" {
			log.Println("ERROR: HASURA_JWT_TYPE is RS256 but HASURA_JWK_URL is missing.")
			return nil, errors.New("missing required configuration: HASURA_JWK_URL for RS256")
		}
	default:
		log.Printf("ERROR: Unsupported HASURA_JWT_TYPE configured: %s", cfg.HasuraJWTType)
		return nil, fmt.Errorf("unsupported HASURA_JWT_TYPE: %s", cfg.HasuraJWTType)
	}

	AppConfig = &cfg
	log.Println("INFO: Configuration loaded successfully.")
	return AppConfig, nil
}
