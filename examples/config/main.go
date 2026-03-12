// Example: loading configuration from environment variables and a YAML file.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/qwackididuck/duck/config"
)

// AppConfig is the application configuration struct.
// Duck tags control how each field is resolved.
type AppConfig struct {
	// Loaded from ADDR env var, defaults to :8080
	Addr string `duck:"default=:8080" env:"ADDR"`

	// Loaded from LOG_LEVEL env var, defaults to info
	LogLevel string `duck:"default=info" env:"LOG_LEVEL"`

	// Loaded from SHUTDOWN_TIMEOUT env var, duration parsing included
	ShutdownTimeout time.Duration `duck:"default=30s" env:"SHUTDOWN_TIMEOUT"`

	// Required — Load returns an error if DATABASE_URL is not set
	DatabaseURL string `duck:"required" env:"DATABASE_URL"`

	// Comma-separated list from env: TAGS=api,backend,v2
	Tags []string `duck:"sep=," env:"TAGS"`

	// Redis connection string
	RedisAddr string `duck:"default=localhost:6379" env:"REDIS_ADDR"`
}

func main() {
	// Simulate env vars being set
	_ = os.Setenv("DATABASE_URL", "postgres://localhost:5432/myapp")
	_ = os.Setenv("TAGS", "api,backend,v2")
	_ = os.Setenv("LOG_LEVEL", "debug")

	// Load from env vars + a config file.
	// Priority: env > file > defaults.
	cfg, err := config.Load[AppConfig](
		config.WithEnv(),
		// config.WithFile("config.yaml"), // uncomment to also load from file
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Addr:            %s\n", cfg.Addr)
	fmt.Printf("LogLevel:        %s\n", cfg.LogLevel)
	fmt.Printf("ShutdownTimeout: %s\n", cfg.ShutdownTimeout)
	fmt.Printf("DatabaseURL:     %s\n", cfg.DatabaseURL)
	fmt.Printf("Tags:            %v\n", cfg.Tags)
	fmt.Printf("RedisAddr:       %s\n", cfg.RedisAddr)
}
