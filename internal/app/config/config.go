package config

import "os"

// Config holds application configuration.
// All values are loaded from environment or config files — no hardcoded defaults in modules.
type Config struct {
	Port        string
	DatabaseURL string
	Environment string
	LogLevel    string
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		Environment: getEnv("ENVIRONMENT", "development"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
