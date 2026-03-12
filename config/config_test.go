package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qwackididuck/duck/config"
)

// --- Helpers ---

func writeFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	return path
}

// --- Shared config types ---

type stringConfig struct {
	Port    string `duck:"default=8080"                         env:"PORT"`
	DBUrl   string `duck:"required"                             env:"DB_URL"`
	AppName string `duck:"required,errMsg=APP_NAME is required" env:"APP_NAME"`
	Debug   string `duck:"default=false"                        env:"DEBUG"`
}

type typedConfig struct {
	Port    int           `duck:"default=8080"  env:"PORT"`
	Debug   bool          `duck:"default=false" env:"DEBUG"`
	Timeout time.Duration `duck:"default=30s"   env:"TIMEOUT"`
	Rate    float64       `duck:"default=1.5"   env:"RATE"`
	Secret  []byte        `duck:"required"      env:"SECRET"`
	Workers uint          `duck:"default=4"     env:"WORKERS"`
}

type sliceConfig struct {
	Tags     []string        `duck:"default=a,b,c" env:"TAGS"`
	Ports    []int           `duck:"sep=:"         env:"PORTS"`
	Timeouts []time.Duration `env:"TIMEOUTS"`
}

// --- New() / tag validation ---

func TestLoad_tagValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		load    func() error
		wantErr error
	}{
		{
			name: "sep= on non-slice returns ErrInvalidTag",
			load: func() error {
				type badConfig struct {
					NotASlice string `duck:"sep=;" env:"LEGUMES"`
				}

				_, err := config.Load[badConfig](config.WithEnv())

				return err
			},
			wantErr: config.ErrInvalidTag,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.load()
			if err == nil {
				t.Fatalf("Load() expected error, got nil")
			}

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got: %v", tc.wantErr, err)
			}
		})
	}
}

// --- Load from env ---

func TestLoad_fromEnv(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantErr     error
		checkResult func(t *testing.T, cfg stringConfig)
	}{
		{
			name: "all fields from env",
			env: map[string]string{
				"PORT":     "9090",
				"DB_URL":   "postgres://localhost/db",
				"APP_NAME": "myapp",
			},
			checkResult: func(t *testing.T, cfg stringConfig) {
				t.Helper()

				if cfg.Port != "9090" {
					t.Errorf("Port: expected %q, got %q", "9090", cfg.Port)
				}

				if cfg.DBUrl != "postgres://localhost/db" {
					t.Errorf("DBUrl: expected %q, got %q", "postgres://localhost/db", cfg.DBUrl)
				}
			},
		},
		{
			name: "default applied when env missing",
			env: map[string]string{
				"DB_URL":   "postgres://localhost/db",
				"APP_NAME": "myapp",
			},
			checkResult: func(t *testing.T, cfg stringConfig) {
				t.Helper()

				if cfg.Port != "8080" {
					t.Errorf("Port: expected default %q, got %q", "8080", cfg.Port)
				}
			},
		},
		{
			name:    "missing required field returns ErrMissingMandatory",
			env:     map[string]string{},
			wantErr: config.ErrMissingMandatory,
		},
		{
			name:    "missing required with custom errMsg",
			env:     map[string]string{"DB_URL": "postgres://localhost/db"},
			wantErr: config.ErrMissingMandatory,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := config.Load[stringConfig](config.WithEnv())

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("expected %v, got: %v", tc.wantErr, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if tc.checkResult != nil {
				tc.checkResult(t, cfg)
			}
		})
	}
}

// --- Type conversion ---
//
//nolint:gocyclo,cyclop
func TestLoad_typeConversion(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantErr     error
		checkResult func(t *testing.T, cfg typedConfig)
	}{
		{
			name: "all scalar types converted correctly",
			env: map[string]string{
				"PORT":    "9090",
				"DEBUG":   "true",
				"TIMEOUT": "1m",
				"RATE":    "3.14",
				"SECRET":  "mysecret",
				"WORKERS": "8",
			},
			checkResult: func(t *testing.T, cfg typedConfig) {
				t.Helper()

				if cfg.Port != 9090 {
					t.Errorf("Port: expected 9090, got %d", cfg.Port)
				}

				if !cfg.Debug {
					t.Error("Debug: expected true")
				}

				if cfg.Timeout != time.Minute {
					t.Errorf("Timeout: expected 1m, got %v", cfg.Timeout)
				}

				if cfg.Rate != 3.14 {
					t.Errorf("Rate: expected 3.14, got %v", cfg.Rate)
				}

				if string(cfg.Secret) != "mysecret" {
					t.Errorf("Secret: expected %q, got %q", "mysecret", string(cfg.Secret))
				}

				if cfg.Workers != 8 {
					t.Errorf("Workers: expected 8, got %d", cfg.Workers)
				}
			},
		},
		{
			name: "defaults applied for unset fields",
			env:  map[string]string{"SECRET": "s"},
			checkResult: func(t *testing.T, cfg typedConfig) {
				t.Helper()

				if cfg.Port != 8080 {
					t.Errorf("Port: expected 8080, got %d", cfg.Port)
				}

				if cfg.Timeout != 30*time.Second {
					t.Errorf("Timeout: expected 30s, got %v", cfg.Timeout)
				}

				if cfg.Workers != 4 {
					t.Errorf("Workers: expected 4, got %d", cfg.Workers)
				}
			},
		},
		{
			name:    "invalid int returns ErrConversion",
			env:     map[string]string{"PORT": "not-a-number", "SECRET": "s"},
			wantErr: config.ErrConversion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := config.Load[typedConfig](config.WithEnv())

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("expected %v, got: %v", tc.wantErr, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if tc.checkResult != nil {
				tc.checkResult(t, cfg)
			}
		})
	}
}

// --- Panic behavior ---
//
//nolint:paralleltest // cannot use t.Parallel with t.Setenv
func TestLoad_panicBehavior(t *testing.T) {
	tests := []struct {
		name      string
		setupEnv  func(t *testing.T)
		load      func() error
		wantPanic bool
	}{
		{
			name:     "required+panic panics on missing field",
			setupEnv: func(_ *testing.T) {},
			load: func() error {
				type panicConfig struct {
					Secret string `duck:"required,panic" env:"PANIC_SECRET"`
				}

				_, err := config.Load[panicConfig](config.WithEnv())

				return err
			},
			wantPanic: true,
		},
		{
			name: "conversion+panic panics on bad value",
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("PANIC_PORT", "not-a-number")
			},
			load: func() error {
				type panicConfig struct {
					Port int `duck:"panic" env:"PANIC_PORT"`
				}

				_, err := config.Load[panicConfig](config.WithEnv())

				return err
			},
			wantPanic: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupEnv(t)

			if tc.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Error("expected panic, got none")
					}
				}()
			}

			_ = tc.load()
		})
	}
}

// --- Slices ---

func TestLoad_slices(t *testing.T) { //nolint:cyclop // table-driven test with inline assertions — complexity reflects test coverage, not logic
	tests := []struct {
		name        string
		env         map[string]string
		checkResult func(t *testing.T, cfg sliceConfig)
	}{
		{
			name: "comma-separated string slice from env",
			env: map[string]string{
				"TAGS": "go,duck,toolkit",
			},
			checkResult: func(t *testing.T, cfg sliceConfig) {
				t.Helper()

				if len(cfg.Tags) != 3 || cfg.Tags[0] != "go" || cfg.Tags[2] != "toolkit" {
					t.Errorf("Tags: expected [go duck toolkit], got %v", cfg.Tags)
				}
			},
		},
		{
			name: "custom separator for int slice",
			env: map[string]string{
				"TAGS":  "a",
				"PORTS": "8080:9090:7070",
			},
			checkResult: func(t *testing.T, cfg sliceConfig) {
				t.Helper()

				if len(cfg.Ports) != 3 || cfg.Ports[1] != 9090 {
					t.Errorf("Ports: expected [8080 9090 7070], got %v", cfg.Ports)
				}
			},
		},
		{
			name: "duration slice from env",
			env: map[string]string{
				"TAGS":     "a",
				"TIMEOUTS": "1s,2s,30s",
			},
			checkResult: func(t *testing.T, cfg sliceConfig) {
				t.Helper()

				if len(cfg.Timeouts) != 3 || cfg.Timeouts[2] != 30*time.Second {
					t.Errorf("Timeouts: expected [1s 2s 30s], got %v", cfg.Timeouts)
				}
			},
		},
		{
			name: "default slice applied when env missing",
			env:  map[string]string{},
			checkResult: func(t *testing.T, cfg sliceConfig) {
				t.Helper()

				if len(cfg.Tags) != 3 || cfg.Tags[2] != "c" {
					t.Errorf("Tags default: expected [a b c], got %v", cfg.Tags)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Always clear slice-related env vars so subtests don't bleed into each other.
			t.Setenv("TAGS", "")
			t.Setenv("PORTS", "")
			t.Setenv("TIMEOUTS", "")

			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := config.Load[sliceConfig](config.WithEnv())
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if tc.checkResult != nil {
				tc.checkResult(t, cfg)
			}
		})
	}
}

// --- File sources ---

func TestLoad_fromFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filename    string
		content     string
		checkResult func(t *testing.T, cfg stringConfig)
		wantErr     bool
	}{
		{
			name:     "JSON file",
			filename: "config.json",
			content:  `{"PORT":"7070","DB_URL":"postgres://file/db","APP_NAME":"from-file"}`,
			checkResult: func(t *testing.T, cfg stringConfig) {
				t.Helper()

				if cfg.Port != "7070" {
					t.Errorf("Port: expected %q, got %q", "7070", cfg.Port)
				}

				if cfg.DBUrl != "postgres://file/db" {
					t.Errorf("DBUrl: expected %q, got %q", "postgres://file/db", cfg.DBUrl)
				}
			},
		},
		{
			name:     "YAML file",
			filename: "config.yaml",
			content:  "PORT: \"5050\"\nDB_URL: \"postgres://yaml/db\"\nAPP_NAME: from-yaml\n",
			checkResult: func(t *testing.T, cfg stringConfig) {
				t.Helper()

				if cfg.Port != "5050" {
					t.Errorf("Port: expected %q, got %q", "5050", cfg.Port)
				}
			},
		},
		{
			name:     "missing file returns error",
			filename: "",
			content:  "",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var path string
			if tc.filename != "" {
				path = writeFile(t, tc.filename, tc.content)
			} else {
				path = "/nonexistent/config.json"
			}

			cfg, err := config.Load[stringConfig](config.WithFile(path))

			if tc.wantErr {
				if err == nil {
					t.Fatal("Load() expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if tc.checkResult != nil {
				tc.checkResult(t, cfg)
			}
		})
	}
}

// --- Native arrays in files ---

func TestLoad_nativeArraysInFiles(t *testing.T) {
	t.Parallel()

	type arrayFileConfig struct {
		Tags []string `env:"TAGS"`
	}

	tests := []struct {
		name     string
		filename string
		content  string
		wantTags []string
	}{
		{
			name:     "YAML native array",
			filename: "config.yaml",
			content:  "TAGS:\n  - patate\n  - carotte\n  - tomate\n",
			wantTags: []string{"patate", "carotte", "tomate"},
		},
		{
			name:     "JSON native array",
			filename: "config.json",
			content:  `{"TAGS":["go","duck","toolkit"]}`,
			wantTags: []string{"go", "duck", "toolkit"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeFile(t, tc.filename, tc.content)

			cfg, err := config.Load[arrayFileConfig](config.WithFile(path))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if len(cfg.Tags) != len(tc.wantTags) {
				t.Fatalf("Tags: expected %v, got %v", tc.wantTags, cfg.Tags)
			}

			for i, want := range tc.wantTags {
				if cfg.Tags[i] != want {
					t.Errorf("Tags[%d]: expected %q, got %q", i, want, cfg.Tags[i])
				}
			}
		})
	}
}

// --- Priority: env over file ---

func TestLoad_envOverridesFile(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		filename    string
		env         map[string]string
		checkResult func(t *testing.T, cfg stringConfig)
	}{
		{
			name:        "env overrides JSON file for scalar",
			filename:    "config.json",
			fileContent: `{"PORT":"1111","DB_URL":"postgres://file/db","APP_NAME":"from-file"}`,
			env:         map[string]string{"PORT": "2222", "DB_URL": "postgres://env/db", "APP_NAME": "from-env"},
			checkResult: func(t *testing.T, cfg stringConfig) {
				t.Helper()

				if cfg.Port != "2222" {
					t.Errorf("Port: expected env value %q, got %q", "2222", cfg.Port)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, tc.filename, tc.fileContent)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := config.Load[stringConfig](config.WithEnv(), config.WithFile(path))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if tc.checkResult != nil {
				tc.checkResult(t, cfg)
			}
		})
	}
}

// Dedicated test for env overriding file on a slice field.
func TestLoad_envOverridesFile_slice(t *testing.T) {
	type slicePriorityConfig struct {
		Tags []string `env:"TAGS"`
	}

	path := writeFile(t, "config.yaml", "TAGS:\n  - from-file-a\n  - from-file-b\n  - from-file-c\n")

	t.Setenv("TAGS", "from-env-x,from-env-y")

	cfg, err := config.Load[slicePriorityConfig](config.WithEnv(), config.WithFile(path))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if len(cfg.Tags) != 2 || cfg.Tags[0] != "from-env-x" {
		t.Errorf("Tags: expected [from-env-x from-env-y], got %v", cfg.Tags)
	}
}

// --- MustLoad ---
//
//nolint:paralleltest // cannot use t.Parallel with t.Setenv
func TestMustLoad(t *testing.T) {
	tests := []struct {
		name      string
		setupEnv  func(t *testing.T)
		wantPanic bool
	}{
		{
			name:      "panics on missing required field",
			setupEnv:  func(_ *testing.T) {},
			wantPanic: true,
		},
		{
			name: "succeeds with all required fields set",
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("MUSTLOAD_DB_URL", "postgres://localhost/db")
				t.Setenv("MUSTLOAD_APP_NAME", "myapp")
			},
			wantPanic: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupEnv(t)

			type mustConfig struct {
				DBUrl   string `duck:"required" env:"MUSTLOAD_DB_URL"`
				AppName string `duck:"required" env:"MUSTLOAD_APP_NAME"`
			}

			if tc.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Error("MustLoad() expected panic, got none")
					}
				}()
			}

			_ = config.MustLoad[mustConfig](config.WithEnv())
		})
	}
}
