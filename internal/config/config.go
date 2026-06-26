package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Port     int
	Database DatabaseConfig
	JWT      JWTConfig
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
	ExpiresIn string
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

	cfg := &Config{
		Port: viper.GetInt("PORT"),
		Database: DatabaseConfig{
			Host:     viper.GetString("DB_HOST"),
			Port:     viper.GetInt("DB_PORT"),
			User:     viper.GetString("DB_USER"),
			Password: viper.GetString("DB_PASS"),
			Name:     viper.GetString("DB_NAME"),
		},
		JWT: JWTConfig{
			Secret:    viper.GetString("JWT_SECRET"),
			ExpiresIn: "1h",
		},
	}

	if cfg.Port == 0 {
		cfg.Port = 3000
	}

	return cfg, nil
}
