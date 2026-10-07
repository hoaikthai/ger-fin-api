package config

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// minJWTSecretLen is the minimum HS256 secret length; shorter secrets can be
// brute-forced offline from any captured token.
const minJWTSecretLen = 32

type Config struct {
	Port     int
	Database DatabaseConfig
	JWT      JWTConfig
	OXRAppID string
	// CORSAllowedOrigins from CORS_ALLOWED_ORIGINS (comma-separated); empty allows all origins.
	CORSAllowedOrigins []string
	// TrustedProxies from TRUSTED_PROXIES (comma-separated IPs/CIDRs); empty trusts none,
	// so client IPs (used for rate limiting) cannot be spoofed via X-Forwarded-For.
	TrustedProxies []string
}

type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

type JWTConfig struct {
	Secret    string
	ExpiresIn time.Duration
	// RefreshTTL from REFRESH_TOKEN_TTL; each refresh extends the session by this much.
	RefreshTTL time.Duration
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
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
	if len(secret) < minJWTSecretLen {
		return nil, fmt.Errorf("JWT_SECRET must be at least %d bytes (generate one with: openssl rand -base64 48)", minJWTSecretLen)
	}

	expiresIn := time.Hour
	if raw := viper.GetString("JWT_EXPIRES_IN"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			expiresIn = d
		}
	}

	refreshTTL := 90 * 24 * time.Hour
	if raw := viper.GetString("REFRESH_TOKEN_TTL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			refreshTTL = d
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
			SSLMode:  dbSSLMode(),
		},
		JWT: JWTConfig{
			Secret:     secret,
			ExpiresIn:  expiresIn,
			RefreshTTL: refreshTTL,
		},
	}

	cfg.CORSAllowedOrigins = splitList(viper.GetString("CORS_ALLOWED_ORIGINS"))
	cfg.TrustedProxies = splitList(viper.GetString("TRUSTED_PROXIES"))

	cfg.OXRAppID = viper.GetString("OXR_APP_ID")
	if cfg.OXRAppID == "" {
		log.Println("OXR_APP_ID not set — currency rate fetching disabled")
	}

	return cfg, nil
}

// dbSSLMode reads DB_SSLMODE, defaulting to "disable" for local development.
// Set it to "require" (or stricter) when the database is on another host.
func dbSSLMode() string {
	if m := viper.GetString("DB_SSLMODE"); m != "" {
		return m
	}
	return "disable"
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
