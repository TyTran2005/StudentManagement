package config

import (
	"log"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	DBHost         string        `mapstructure:"DB_HOST"`
	DBPort         string        `mapstructure:"DB_PORT"`
	DBUser         string        `mapstructure:"DB_USER"`
	DBPassword     string        `mapstructure:"DB_PASSWORD"`
	DBName         string        `mapstructure:"DB_NAME"`
	DBSslmode      string        `mapstructure:"DB_SSLMODE"`
	ServerPort     string        `mapstructure:"SERVER_PORT"`
	JWTSecretKey   string        `mapstructure:"JWT_SECRET_KEY"`
	JWTExpiresHour time.Duration `mapstructure:"JWT_EXPIRATION_HOURS"`
}

var AppConfig *Config

func LoadConfig() (*Config, error) {
	viper.AddConfigPath(".")    
	viper.SetConfigName(".env") 
	viper.SetConfigType("env")  

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Println(".env file not found, relying on environment variables")
		} else {
			return nil, err
		}
	}

	viper.SetDefault("SERVER_PORT", "8080")
	viper.SetDefault("JWT_EXPIRATION_HOURS", 72)

	err := viper.Unmarshal(&AppConfig)
	if err != nil {
		return nil, err
	}

	durationStr := viper.GetString("JWT_EXPIRATION_HOURS")
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		log.Printf("Invalid JWT_EXPIRATION_HOURS value: %s, using default 72h", durationStr)
		duration = 72 * time.Hour
	}
	AppConfig.JWTExpiresHour = duration

	if AppConfig.DBUser == "" || AppConfig.DBPassword == "" || AppConfig.DBName == "" || AppConfig.JWTSecretKey == "" {
		log.Println("Warning: One or more critical environment variables (DB_USER, DB_PASSWORD, DB_NAME, JWT_SECRET_KEY) are missing.")
	}

	return AppConfig, nil
}
