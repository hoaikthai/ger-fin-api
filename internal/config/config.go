package config

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Port     int
	Database DatabaseConfig
	JWT      JWTConfig
	OXRAppID string
}

type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
}

type JWTConfig struct {
	Secret    string
	ExpiresIn time.Duration
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, d.User, d.Password, d.Name,
	)
}

func Load() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	viper.AutomaticEnv()

	// Ignore error if .env file not found (rely on real env vars in production)
	_ = viper.ReadInConfig()

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	secret := viper.GetString("JWT_SECRET")
	if secret == "" {
		return nil, errors.New("JWT_SECRET must be set")
	}

	expiresIn := time.Hour
	if raw := viper.GetString("JWT_EXPIRES_IN"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			expiresIn = d
		}
	}

	port := viper.GetInt("PORT")
	if port == 0 {
		port = 3000
	}

	cfg := &Config{
		Port: port,
		Database: DatabaseConfig{
			Host:     viper.GetString("DB_HOST"),
			Port:     viper.GetInt("DB_PORT"),
			User:     viper.GetString("DB_USER"),
			Password: viper.GetString("DB_PASS"),
			Name:     viper.GetString("DB_NAME"),
		},
		JWT: JWTConfig{
			Secret:    secret,
			ExpiresIn: expiresIn,
		},
	}

	cfg.OXRAppID = viper.GetString("OXR_APP_ID")
	if cfg.OXRAppID == "" {
		log.Println("OXR_APP_ID not set — currency rate fetching disabled")
	}

	return cfg, nil
}
