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
	Addr string `duck:"env=ADDR,default=:8080"`

	// Loaded from LOG_LEVEL env var, defaults to info
	LogLevel string `duck:"env=LOG_LEVEL,default=info"`

	// Loaded from SHUTDOWN_TIMEOUT env var, duration parsing included
	ShutdownTimeout time.Duration `duck:"env=SHUTDOWN_TIMEOUT,default=30s"`

	// Mandatory — Load returns an error if DATABASE_URL is not set
	DatabaseURL string `duck:"env=DATABASE_URL,mandatory"`

	// Comma-separated list from env: TAGS=api,backend,v2
	Tags []string `duck:"env=TAGS,sep=,"`

	// Nested struct — fields resolved independently
	Redis RedisConfig
}

type RedisConfig struct {
	Addr     string `duck:"env=REDIS_ADDR,default=localhost:6379"`
	Password string `duck:"env=REDIS_PASSWORD"`
	DB       int    `duck:"env=REDIS_DB,default=0"`
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
	fmt.Printf("Redis.Addr:      %s\n", cfg.Redis.Addr)
}
