<div align="center">

<img src="./docs/assets/logo.png" alt="duck logo" width="256" />

# duck

**A modular toolkit for building production-ready Go HTTP services.**

[![Go version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/doc/go1.26)
[![golangci-lint](https://img.shields.io/badge/golangci--lint-v2.10.1-4B32C3?style=flat-square&logo=github-actions&logoColor=white)](https://github.com/golangci/golangci-lint/releases/tag/v2.10.1)
[![Go Report Card](https://goreportcard.com/badge/github.com/qwackididuck/duck?style=flat-square)](https://goreportcard.com/report/github.com/qwackididuck/duck)
[![License: MIT](https://img.shields.io/badge/License-MIT-22c55e?style=flat-square)](./LICENSE)
[![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/qwackididuck/duck?utm_source=oss&utm_medium=github&utm_campaign=qwackididuck%2Fduck&labelColor=171717&color=FF570A&link=https%3A%2F%2Fcoderabbit.ai&label=CodeRabbit+Reviews)](https://coderabbit.ai)
[![CodeRabbit](https://img.shields.io/badge/code%20review-CodeRabbit-orange?style=flat-square&logo=rabbit&logoColor=white)](https://coderabbit.ai)

Each package is independent — pull in only what you need.

[Installation](#installation) · [Packages](#packages) · [Examples](#examples) · [Design principles](#design-principles)

</div>

---

## Overview

Duck covers the recurring concerns of a Go HTTP service without dictating your architecture. Every package is a standalone import. There is no core you must adopt to use any one piece.

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              your service                                │
├─────────────┬────────────┬────────────┬────────────┬────────────────────┤
│   server    │    log     │   config   │    jwt     │       oauth2       │
│  graceful   │  slog  +   │  env vars  │ go-jose v4 │  Auth Code + PKCE  │
│  shutdown   │  context   │  + files   │  + JWKS    │  session store     │
├─────────────┴────────────┴────────────┴────────────┴────────────────────┤
│             middleware: logging · metrics · body limit · compress        │
├──────────────────────────────────────────────────────────────────────────┤
│                     httpclient: retry · backoff · logging                │
├────────────────────────────────┬─────────────────────────────────────────┤
│       metrics / prometheus     │   oauth2/store: memory · redis          │
└────────────────────────────────┴─────────────────────────────────────────┘
```

### Package index

| Package                         | What it does                                                            |
|---------------------------------|-------------------------------------------------------------------------|
| [`server`](#server)             | HTTP server — graceful shutdown, goroutine supervision, health probes   |
| [`log`](#log)                   | `*slog.Logger` factory with context-based attribute propagation         |
| [`config`](#config)             | Generic config loader from env vars and JSON/YAML files                 |
| [`statemachine`](#statemachine) | Typed state machine with panic recovery and context cancellation        |
| [`middleware`](#middleware)     | `Logging` · `BodyLimit` · `Compress` · `HTTPMetrics`                    |
| [`metrics`](#metrics)           | Prometheus backend for the `HTTPMetrics` middleware                     |
| [`jwt`](#jwt)                   | JWT generation and validation — HMAC, RSA, ECDSA, JWKS, key rotation    |
| [`oauth2`](#oauth2)             | OAuth2/OIDC authentication — Google, GitHub, extensible to any provider |
| [`httpclient`](#httpclient)     | `*http.Client` with pluggable retry and logging transports              |

---

## Installation

```bash
go get github.com/qwackididuck/duck
```

**Requires Go 1.26 or later.**

Sub-packages are separate imports so you only bring in the dependencies you actually use:

```bash
go get github.com/qwackididuck/duck/server       # no extra dependencies
go get github.com/qwackididuck/duck/jwt          # pulls go-jose/v4
go get github.com/qwackididuck/duck/oauth2       # pulls golang.org/x/oauth2
go get github.com/qwackididuck/duck/oauth2/store # + redis (optional)
go get github.com/qwackididuck/duck/metrics      # + prometheus/client_golang
```

---

## Packages

### server

HTTP server with graceful shutdown, background goroutine lifecycle management, and optional liveness/readiness probes.

#### Basic setup

```go
import "github.com/qwackididuck/duck/server"

srv, err := server.New(
    server.WithAddr(":8080"),
    server.WithHandler(router),
    server.WithLogger(logger),
    server.WithShutdownTimeout(30 * time.Second),
)
if err != nil {
    log.Fatal(err)
}

if err := srv.Start(); err != nil { // blocks until SIGINT / SIGTERM
    log.Fatal(err)
}
```

#### Graceful shutdown sequence

When `SIGINT` or `SIGTERM` is received:

1. The server stops accepting new connections.
2. The app context is cancelled — all goroutines registered via `Go()` receive the signal.
3. `http.Server.Shutdown` drains in-flight HTTP requests.
4. The server waits up to `ShutdownTimeout` for all `Go()` goroutines to return.
5. The process exits cleanly.

#### Background goroutines

```go
// ctx is cancelled at step 2 above — use it to stop gracefully.
srv.Go(func(ctx context.Context) {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            runPeriodicJob(ctx)
        }
    }
})
```

#### Health check probes

> Designed for Kubernetes liveness and readiness probes, but usable with any load balancer or orchestrator.

```go
srv, err := server.New(
    server.WithAddr(":8080"),
    server.WithHandler(router),

    // Activates GET /health and GET /ready
    server.WithHealthChecks("payments-service",
        // 503 on KO — removes pod from load balancer pool automatically.
        // Omit to return 200 instead (e.g. if probes are not used by an orchestrator).
        server.WithKOStatus(http.StatusServiceUnavailable),
    ),

    // Each dependency is checked sequentially on every /ready request.
    server.WithDependency(&PostgresChecker{db: db}),
    server.WithDependency(&RedisChecker{client: rdb}),
    server.WithDependency(&PaymentGatewayChecker{url: gatewayURL}),
)
```

Implement `server.Stater` for each dependency. Duck calls `Status()` as-is — you are responsible for your own timeout:

```go
type PostgresChecker struct{ db *sql.DB }

func (c *PostgresChecker) Status(ctx context.Context) server.ServiceStatus {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()

    if err := c.db.PingContext(ctx); err != nil {
        return server.ServiceStatus{Name: "postgres", Status: server.StatusKO}
    }

    return server.ServiceStatus{Name: "postgres", Status: server.StatusOK}
}
```

`GET /health` — liveness probe. Always `OK` while the process is running:

```json
{
  "name": "payments-service",
  "status": "OK"
}
```

`GET /ready` — readiness probe. Checks every registered dependency:

```json
{
  "name": "payments-service",
  "status": "KO",
  "services": [
    { "name": "postgres",        "status": "OK" },
    { "name": "redis",           "status": "OK" },
    { "name": "payment-gateway", "status": "KO" }
  ]
}
```

> If any dependency returns `KO`, the overall status becomes `KO`.

#### Options

| Option | Default | Description |
|---|---|---|
| `WithAddr(addr)` | `:8080` | TCP address to listen on |
| `WithHandler(h)` | `http.DefaultServeMux` | Your router with middlewares applied |
| `WithLogger(l)` | `slog.Default()` | Structured logger for lifecycle events |
| `WithShutdownTimeout(d)` | `30s` | Max time to wait for in-flight requests and goroutines |
| `WithReadHeaderTimeout(d)` | `10s` | Prevents Slowloris attacks |
| `WithBaseContext(ctx)` | `context.Background()` | Parent context for all request contexts |
| `WithHealthChecks(name, opts...)` | — | Enable `/health` and `/ready` |
| `WithDependency(stater)` | — | Register a dependency checked by `/ready` |
| `WithKOStatus(code)` | `200` | HTTP status when a dependency is KO |

---

### log

A thin construction and context-propagation layer on top of `log/slog`. Returns standard `*slog.Logger` — no custom type to carry through your codebase.

#### Building a logger

```go
import ducklog "github.com/qwackididuck/duck/log"

logger := ducklog.New(
    ducklog.WithFormat(ducklog.FormatJSON),  // FormatJSON (default) or FormatText
    ducklog.WithLevel(slog.LevelInfo),
    ducklog.WithOutput(os.Stdout),
)
```

#### Context propagation

Attach attributes to a context once — they appear automatically in every subsequent log call that uses that context.

```go
// Typically done in your request logging middleware, once per request:
ctx := ducklog.ContextWithAttrs(r.Context(),
    slog.String("request_id", requestID),
    slog.String("user_id",    session.UserID),
    slog.String("tenant",     tenantID),
)

// In a handler — no need to repeat request_id, user_id, tenant:
ducklog.FromContext(ctx, logger).Info("order created",
    slog.String("order_id", "ord_123"),
    slog.Float64("amount", 49.99),
)
```

JSON output:

```json
{
  "level": "INFO",
  "msg": "order created",
  "request_id": "req-3f8a",
  "user_id": "usr_42",
  "tenant": "acme",
  "order_id": "ord_123",
  "amount": 49.99
}
```

#### Options

| Option | Default | Description |
|---|---|---|
| `WithFormat(f)` | `FormatJSON` | Output format — `FormatJSON` or `FormatText` |
| `WithLevel(l)` | `slog.LevelInfo` | Minimum log level |
| `WithOutput(w)` | `os.Stdout` | Output writer |

---

### config

Generic configuration loader powered by Go generics. Resolves values from environment variables and/or JSON/YAML config files. Field behaviour is declared entirely in struct tags.

#### Defining a config struct

```go
import "github.com/qwackididuck/duck/config"

type Config struct {
    Addr            string        `duck:"env=ADDR,default=:8080"`
    LogLevel        string        `duck:"env=LOG_LEVEL,default=info"`
    ShutdownTimeout time.Duration `duck:"env=SHUTDOWN_TIMEOUT,default=30s"`

    // mandatory — Load returns an error if the variable is not set
    DatabaseURL string `duck:"env=DATABASE_URL,mandatory"`

    // comma-separated from env:  ALLOWED_ORIGINS=https://a.com,https://b.com
    AllowedOrigins []string `duck:"env=ALLOWED_ORIGINS,sep=,"`

    // nested struct — fields resolved independently
    Redis RedisConfig
}

type RedisConfig struct {
    Addr     string `duck:"env=REDIS_ADDR,default=localhost:6379"`
    Password string `duck:"env=REDIS_PASSWORD"`
    DB       int    `duck:"env=REDIS_DB,default=0"`
}
```

#### Loading

```go
cfg, err := config.Load[Config](
    config.WithEnv(),
    config.WithFile("config.yaml"), // optional — omit if env-only
)
if err != nil {
    log.Fatal(err)
}
```

Resolution order: **environment variables** → **config file** → **tag defaults**

> If a mandatory field is absent from all sources, `Load` returns an error wrapping `config.ErrMissingMandatory`.

#### Tag reference

| Tag | Type | Description | Example |
|---|---|---|---|
| `env=NAME` | string | Environment variable name | `env=DATABASE_URL` |
| `default=value` | any | Fallback value | `default=:8080`, `default=30s` |
| `mandatory` | flag | Error if not resolved | `mandatory` |
| `sep=char` | slice only | Separator for env var parsing | `sep=,` |

> `sep=` applies only to env var values. JSON/YAML files use native array syntax and ignore `sep=`.

#### Equivalent YAML file

```yaml
addr: ":9090"
log_level: "debug"
shutdown_timeout: "60s"
database_url: "postgres://localhost:5432/myapp?sslmode=disable"
allowed_origins:
  - "https://app.example.com"
  - "https://admin.example.com"
redis:
  addr: "redis:6379"
  db: 1
```

---

### statemachine

A typed, generic state machine. States are plain functions — no interface to implement. Each state returns the next state to execute, or `nil` to terminate. Panics are recovered and returned as errors. The machine checks for context cancellation between every transition.

#### Defining states

```go
import "github.com/qwackididuck/duck/statemachine"

// D is your data type — passed by value and returned by Run.
type OrderData struct {
    OrderID string
    Total   float64
    Email   string
    Paid    bool
}

// StateFunc[D] = func(context.Context, D) StateFunc[D]

func validateOrder(ctx context.Context, d OrderData) statemachine.StateFunc[OrderData] {
    if d.Total <= 0 || d.Email == "" {
        return nil // terminal — validation failed, no transition
    }
    return chargePayment
}

func chargePayment(ctx context.Context, d OrderData) statemachine.StateFunc[OrderData] {
    // call payment provider...
    d.Paid = true
    return sendConfirmation
}

func sendConfirmation(ctx context.Context, d OrderData) statemachine.StateFunc[OrderData] {
    // send email...
    return nil // terminal — all done
}
```

#### Running

```go
result, err := statemachine.Run(ctx, validateOrder, OrderData{
    OrderID: "ord_001",
    Total:   99.99,
    Email:   "alice@example.com",
})
```

`Run` blocks until the machine terminates, the context is cancelled, or a panic is recovered. It returns the data as it was when the last state returned `nil`.

#### Error types

| Error | When |
|---|---|
| `statemachine.ErrPanic` | A state function panicked — the original value and stack trace are embedded in the error message |
| `statemachine.ErrContextDone` | The context was cancelled or its deadline exceeded between two state transitions |

```go
result, err := statemachine.Run(ctx, validateOrder, data)
switch {
case err == nil:
    // success
case errors.Is(err, statemachine.ErrPanic):
    logger.Error("unexpected panic in state machine", "err", err)
case errors.Is(err, statemachine.ErrContextDone):
    logger.Warn("state machine aborted by context", "err", err)
}
```

#### Options

| Option | Default | Description |
|---|---|---|
| `WithTimeout(d)` | — | Apply an additional deadline — the stricter of timeout and context wins |
| `WithLogger(l)` | `slog.Default()` | Logger for recovered panics |

---

### middleware

HTTP server middlewares. Each is independent and can be composed using `middleware.Chain`.

```go
import "github.com/qwackididuck/duck/middleware"

router.Use(middleware.Chain(
    middleware.Logging(logger),
    middleware.BodyLimit(1 * 1024 * 1024),
    middleware.Compress(),
    middleware.HTTPMetrics(promProvider),
))
```

#### Logging

Logs every incoming request and its corresponding response. Generates or propagates `X-Request-Id`. Supports body capture and sensitive data obfuscation.

```go
middleware.Logging(logger,
    middleware.WithRequestBody(true),
    middleware.WithResponseBody(true),
    middleware.WithMaxBodySize(4 * 1024),                          // 4 KB max logged
    middleware.WithObfuscatedHeaders("Authorization", "Cookie"),   // value → "***"
    middleware.WithObfuscatedBodyFields("password", "token", "secret"),
)
```

Sample log output:

```json
{"level":"INFO","msg":"incoming_request","method":"POST","path":"/api/login","request_id":"req-9f3c","body":{"email":"alice@example.com","password":"***"}}
{"level":"INFO","msg":"outgoing_response","status":200,"duration_ms":14,"request_id":"req-9f3c"}
```

#### BodyLimit

Rejects requests whose body exceeds the configured size with `413 Request Entity Too Large`. Uses two-level protection: `Content-Length` header first, then `http.MaxBytesReader` for clients that send an incorrect or missing header.

```go
middleware.BodyLimit(5 * 1024 * 1024) // 5 MB
// 0         → use default (1 MB)
// negative  → disabled
```

#### Compress

Gzip compression with lazy activation — only compresses once the response body reaches `MinSize` and the `Content-Type` is compressible. `gzip.Writer` instances are pooled with `sync.Pool`.

```go
middleware.Compress(
    middleware.WithCompressionLevel(gzip.BestSpeed),
    middleware.WithMinSize(1024),
)
```

Default compressible types: `application/json`, `application/xml`, `text/html`, `text/plain`, `text/xml`.

#### HTTPMetrics

Provider-agnostic HTTP metrics middleware. Supply any implementation of `HTTPMetricsProvider` — duck ships a Prometheus one in [`metrics`](#metrics).

```go
middleware.HTTPMetrics(provider,
    // Normalize path parameters to avoid high cardinality in metric labels.
    // Without this, /users/1 and /users/2 are tracked as separate routes.
    middleware.WithPathCleaner(func(r *http.Request) string {
        return chi.RouteContext(r.Context()).RoutePattern() // → /users/{id}
    }),
    // Attach extra labels extracted from each request.
    middleware.WithLabelsFromRequest(func(r *http.Request) map[string]string {
        return map[string]string{"tenant": r.Header.Get("X-Tenant-Id")}
    }),
)
```

---

### metrics

Prometheus implementation of `middleware.HTTPMetricsProvider`. Exposes an HTTP request counter and a latency histogram.

```go
import "github.com/qwackididuck/duck/metrics"

prom, err := metrics.NewPrometheus("myapp",
    metrics.WithAdditionalLabels("tenant", "region"),
    metrics.WithBuckets([]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}),
    metrics.WithConstLabels(prometheus.Labels{"env": "production"}),
)
if err != nil {
    log.Fatal(err)
}

router.Use(middleware.HTTPMetrics(prom,
    middleware.WithPathCleaner(cleanPath),
    middleware.WithLabelsFromRequest(extractTenant),
))

router.Handle("/metrics", prom.Handler()) // Prometheus scrape endpoint
```

#### Exported metrics

| Metric name | Type | Default labels |
|---|---|---|
| `{namespace}_http_requests_total` | Counter | `route`, `method`, `status_code` |
| `{namespace}_http_request_duration_seconds` | Histogram | `route`, `method`, `status_code` |

Additional labels defined via `WithAdditionalLabels` and `WithLabelsFromRequest` are appended to both metrics.

---

### jwt

JWT generation, parsing, and validation middleware built on [go-jose v4](https://github.com/go-jose/go-jose). Claims are generic — bring your own struct and embed `josejwt.Claims`.

> **Minimum key sizes (go-jose v4 requirement):** HS256 → 32 bytes, HS384 → 48 bytes, HS512 → 64 bytes. `WithHMACKey` returns an explicit error if the secret is too short.

#### Defining claims

```go
import (
    josejwt "github.com/go-jose/go-jose/v4/jwt"
    "github.com/qwackididuck/duck/jwt"
)

type AppClaims struct {
    josejwt.Claims              // sub, exp, iss, aud, iat, nbf
    TenantID string `json:"tenantId"`
    Role     string `json:"role"`
}
```

#### Generating a token

```go
// HMAC — symmetric, simplest setup
provider, err := jwt.WithHMACKey(jwt.HS256, []byte("your-32+-byte-minimum-secret!!"))
if err != nil {
    // secret too short
}

token, err := jwt.Generate(AppClaims{
    Claims: josejwt.Claims{
        Subject:  "user-123",
        Expiry:   josejwt.NewNumericDate(time.Now().Add(time.Hour)),
        Issuer:   "payments-service",
        Audience: josejwt.Audience{"web-app"},
    },
    TenantID: "acme",
    Role:     "admin",
}, provider)
```

#### Validating via middleware

```go
// Required — returns 401 if token is absent or invalid
router.Use(jwt.Middleware[AppClaims](provider))

// Optional — handler is always called; claims may or may not be present
router.With(jwt.Middleware[AppClaims](provider, jwt.WithOptional())).
    Get("/feed", feedHandler)
```

#### Extracting claims in a handler

```go
claims, ok := jwt.ClaimsFromContext[AppClaims](r.Context())
if !ok {
    // only possible on optional routes
    return
}
fmt.Println(claims.Subject, claims.TenantID, claims.Role)
```

#### Supported algorithms

| Algorithm | Signature type | Key pair |
|---|---|---|
| `HS256` `HS384` `HS512` | HMAC | `[]byte` (symmetric) |
| `RS256` `RS384` `RS512` | RSA | `*rsa.PrivateKey` + `*rsa.PublicKey` |
| `ES256` `ES384` `ES512` | ECDSA | `*ecdsa.PrivateKey` + `*ecdsa.PublicKey` |

#### Key rotation (zero downtime)

```go
// During a rotation window, issue new tokens with the new key
// while accepting tokens signed by either key.
newProvider, _ := jwt.WithHMACKey(jwt.HS256, newSecret)
oldProvider, _ := jwt.WithHMACKey(jwt.HS256, oldSecret)

rotatingProvider := jwt.NewMultiKeyProvider(newProvider, oldProvider)
// Signs with newProvider, verifies against both.
```

#### External Identity Providers via JWKS

For tokens issued by Keycloak, Auth0, AWS Cognito, Azure AD, or any OpenID Connect provider. Public keys are fetched from the JWKS endpoint, cached in memory, and refreshed automatically. On refresh failure, the last known good keys are used (stale-on-error).

```go
provider, err := jwt.NewJWKSProvider(
    "https://keycloak.company.com/realms/myrealm/protocol/openid-connect/certs",
    jwt.WithJWKSRefreshInterval(30 * time.Minute),
    jwt.WithJWKSAlgorithms(jwt.RS256),
)

// Drop-in replacement for any other provider
router.Use(jwt.Middleware[AppClaims](provider))
```

---

### oauth2

OAuth2/OIDC authentication implementing the **Authorization Code + PKCE** flow. Handles the full cycle: redirect to provider, CSRF verification, token exchange, identity normalization, session creation, and logout.

#### Authorization Code + PKCE flow

```
Browser                    Your service                    Google / GitHub
   │                            │                                │
   │  GET /auth/google/login    │                                │
   ├───────────────────────────>│                                │
   │                            │  generate state (CSRF nonce)   │
   │                            │  generate code_verifier (PKCE) │
   │  302 → accounts.google.com │  store both in short-TTL cookie│
   │<───────────────────────────│                                │
   │                            │                                │
   │  user authenticates ───────────────────────────────────────>│
   │  user consents to scopes ──────────────────────────────────>│
   │                            │                                │
   │  GET /auth/google/callback?code=xxx&state=yyy               │
   ├───────────────────────────>│                                │
   │                            │  verify state == cookie ✓      │
   │                            │  exchange code + verifier ────>│
   │                            │<─── access_token + id_token ───│
   │                            │  extract user identity          │
   │                            │  call OnLogin() → upsert user  │
   │                            │  create session in store        │
   │                            │  set session cookie             │
   │  302 → /dashboard          │                                │
   │<───────────────────────────│                                │
```

#### Setup

```go
import (
    "github.com/qwackididuck/duck/oauth2"
    "github.com/qwackididuck/duck/oauth2/providers"
    "github.com/qwackididuck/duck/oauth2/store"
)

auth, err := oauth2.New(
    // Providers — register as many as needed
    oauth2.WithProvider(providers.Google(
        os.Getenv("GOOGLE_CLIENT_ID"),
        os.Getenv("GOOGLE_CLIENT_SECRET"),
    )),
    oauth2.WithProvider(providers.GitHub(
        os.Getenv("GITHUB_CLIENT_ID"),
        os.Getenv("GITHUB_CLIENT_SECRET"),
    )),

    // Callback URL — {provider} is substituted dynamically
    oauth2.WithRedirectURL("https://myapp.com/auth/{provider}/callback"),

    // Session store
    oauth2.WithSessionStore(store.NewMemoryStore()),   // development
    // oauth2.WithSessionStore(store.NewRedisStore(rdb)), // production

    // OnLogin is the only hook you need to implement.
    // It is called after every successful OAuth callback.
    oauth2.WithOnLogin(func(ctx context.Context, id oauth2.Identity) (oauth2.SessionData, error) {
        // id.Provider   — "google" or "github"
        // id.ProviderID — user's unique ID at the provider (never changes)
        // id.Email, id.Name, id.AvatarURL

        user, isNew := db.Upsert(id.Provider, id.ProviderID, id.Email, id.Name)
        if isNew {
            logger.InfoContext(ctx, "new user registered", "user_id", user.ID)
        }

        return oauth2.SessionData{UserID: user.ID}, nil
    }),

    oauth2.WithSuccessRedirect("/dashboard"),
    oauth2.WithLogoutRedirect("/"),
    oauth2.WithSessionTTL(7 * 24 * time.Hour),
    oauth2.WithSecureCookies(true), // always true in production (HTTPS)
)
```

#### Mounting routes

```go
// duck mounts four routes automatically:
//
//   GET  /auth/{provider}/login     → redirect to provider
//   GET  /auth/{provider}/callback  → handle redirect back, create session
//   GET  /auth/logout               → destroy current session
//   POST /auth/logout/all           → destroy all sessions for current user
router.Mount("/auth", auth.Routes())
```

#### Protecting routes

```go
// RequireAuth — redirects to provider login if no valid session is found
router.With(auth.RequireAuth()).Get("/dashboard", dashboardHandler)
router.With(auth.RequireAuth()).Get("/settings", settingsHandler)

// LoadSession — loads session if present, always calls the handler
// Use this for pages that behave differently for logged-in users
router.With(auth.LoadSession()).Get("/", homeHandler)
```

#### Accessing the session in handlers

```go
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
    session, _ := oauth2.SessionFromContext(r.Context())
    // session.UserID         — your internal user ID (from OnLogin)
    // session.ImpersonatedBy — set if an admin is impersonating this user
    // session.ExpiresAt      — session expiry time

    user := db.FindByID(session.UserID)
    // ...
}
```

#### Logout from all devices

Useful on password change, account compromise, or admin-initiated revocation.

```go
// Programmatic — from any handler
if err := auth.LogoutAll(r.Context(), session.UserID); err != nil {
    http.Error(w, "failed to revoke sessions", http.StatusInternalServerError)
    return
}

// Or via the mounted route (invalidates only the current user's sessions):
// POST /auth/logout/all
```

#### Session stores

| Store | Package | Use case |
|---|---|---|
| `store.NewMemoryStore()` | `oauth2/store` | Development, tests, single-instance apps |
| `store.NewRedisStore(rdb, opts...)` | `oauth2/store` | Production, multi-instance deployments |
| Custom `oauth2.SessionStore` | — | Postgres, MongoDB, DynamoDB, etc. |

The memory store provides a `GC()` method to prune expired sessions. In production, Redis TTL handles expiration automatically.

```go
// Periodically clean up in-memory sessions
srv.Go(func(ctx context.Context) {
    ticker := time.NewTicker(time.Hour)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:   memStore.GC()
        }
    }
})
```

#### Redis store options

```go
store.NewRedisStore(rdb,
    store.WithKeyPrefix("myapp:sessions:"),  // namespace — useful on shared Redis instances
    store.WithTTL(7 * 24 * time.Hour),       // should match oauth2.WithSessionTTL
)
```

#### Adding a custom provider

Implement `oauth2.Provider` to add Keycloak, Okta, LinkedIn, Apple, or any other OAuth2/OIDC provider:

```go
type KeycloakProvider struct {
    clientID, clientSecret, realmURL string
}

func (k *KeycloakProvider) Name() string         { return "keycloak" }
func (k *KeycloakProvider) ClientID() string     { return k.clientID }
func (k *KeycloakProvider) ClientSecret() string { return k.clientSecret }
func (k *KeycloakProvider) Scopes() []string     { return []string{"openid", "profile", "email"} }

func (k *KeycloakProvider) Endpoint() goauth2.Endpoint {
    return goauth2.Endpoint{
        AuthURL:  k.realmURL + "/protocol/openid-connect/auth",
        TokenURL: k.realmURL + "/protocol/openid-connect/token",
    }
}

func (k *KeycloakProvider) Identity(ctx context.Context, token *goauth2.Token) (oauth2.Identity, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet,
        k.realmURL+"/protocol/openid-connect/userinfo", nil)
    if err != nil {
        return oauth2.Identity{}, err
    }
    req.Header.Set("Authorization", "Bearer "+token.AccessToken)
    // ... decode response and return oauth2.Identity
}
```

#### Options reference

| Option | Default | Description |
|---|---|---|
| `WithProvider(p)` | — | Register a provider (call once per provider) |
| `WithRedirectURL(url)` | — | Callback URL; `{provider}` replaced at runtime |
| `WithSessionStore(s)` | — | **Required.** Session persistence backend |
| `WithOnLogin(fn)` | — | **Required.** Hook called after every successful authentication |
| `WithSessionTTL(d)` | `7d` | How long sessions remain valid |
| `WithSuccessRedirect(url)` | `/` | Where to redirect after successful login |
| `WithLogoutRedirect(url)` | `/` | Where to redirect after logout |
| `WithHTTPClient(c)` | `http.DefaultClient` | HTTP client for token exchange and userinfo requests |
| `WithSecureCookies(b)` | `false` | Sets `Secure` flag on cookies — **must be `true` in production** |

---

### httpclient

`*http.Client` factory with composable `http.RoundTripper` transports. Returns a standard `*http.Client` — pass it to any library that accepts one.

Transport chain: **logging → retry → base transport**

The logging transport sits outermost so every attempt (including retries) is logged individually.

#### Building a client

```go
import "github.com/qwackididuck/duck/httpclient"

client := httpclient.New(
    httpclient.WithTimeout(30 * time.Second),

    httpclient.WithLogging(logger,
        httpclient.WithClientRequestBody(true),
        httpclient.WithClientResponseBody(true),
        httpclient.WithClientMaxBodySize(4 * 1024),
        httpclient.WithClientObfuscatedHeaders("Authorization"),
        httpclient.WithClientObfuscatedBodyFields("password", "api_key"),
    ),

    httpclient.WithRetry(
        []httpclient.RetryCondition{
            httpclient.RetryOnStatusCodes(429, 502, 503, 504),
            httpclient.RetryOnNetworkErrors(),
        },
        httpclient.WithMaxAttempts(3),
        httpclient.WithExponentialBackoff(100 * time.Millisecond),
    ),
)

resp, err := client.Do(req)
```

#### Retry conditions

Conditions are OR-ed — the request is retried if **any** condition is true.

| Condition | When it retries |
|---|---|
| `RetryOnStatusCodes(codes...)` | Response status code is in the list |
| `RetryOnNetworkErrors()` | Connection refused, timeout, EOF, DNS failure |
| `RetryOnIdempotentMethods()` | Any error on GET, PUT, DELETE, HEAD, OPTIONS |
| `RetryIf(fn)` | Custom predicate — `func(*http.Response, error) bool` |

> **Safety guarantee:** `POST` and `PATCH` are never retried on HTTP status errors — only on network errors where the server is guaranteed not to have processed the request, preventing duplicate side effects.

#### Backoff strategies

| Strategy | Wait between attempts |
|---|---|
| `ExponentialBackoff(base)` | `random(0, base × 2ⁿ)` — full jitter, prevents thundering herds |
| `ConstantBackoff(d)` | Fixed duration |
| `NoBackoff()` | Immediate, no wait |

#### Request ID propagation

The logging transport automatically forwards `X-Request-Id` from the outgoing request to the downstream service. Set the header once from your server middleware and it propagates across service boundaries without any extra wiring:

```go
// In your server logging middleware:
req.Header.Set("X-Request-Id", requestID)

// The httpclient logging transport forwards it automatically.
// The downstream service receives X-Request-Id and can log it for correlation.
```

---

## Examples

Runnable examples in [`examples/`](./examples). Each is self-contained and can be run with `go run ./examples/<name>`.

| Example                                            | What it covers                                                          |
|----------------------------------------------------|-------------------------------------------------------------------------|
| [`examples/server`](./examples/server)             | Server, health checks, graceful shutdown, background goroutines         |
| [`examples/config`](./examples/config)             | Env vars, YAML file, mandatory fields, slice parsing with `sep=`        |
| [`examples/log`](./examples/log)                   | Context propagation through multiple handler layers                     |
| [`examples/statemachine`](./examples/statemachine) | Order pipeline — happy path, validation failure, timeout cancellation   |
| [`examples/middleware`](./examples/middleware)     | Full chain with Prometheus, body obfuscation, and chi route patterns    |
| [`examples/jwt`](./examples/jwt)                   | HMAC, RSA, ECDSA, key rotation, JWKS provider                           |
| [`examples/oauth2`](./examples/oauth2)             | Google + GitHub login, DB upsert, memory GC, logout all                 |
| [`examples/httpclient`](./examples/httpclient)     | Retry with simulated failures, custom condition, request ID correlation |

---

## Design principles

**Functional options everywhere.** Configuration is done through `With*` functions. Adding a new option is always backward-compatible — no existing call site breaks.

**Generics for type safety.** `statemachine.Run[D]`, `jwt.Middleware[C]`, `config.Load[T]` — type mismatches are caught at compile time. No `interface{}`, no casting.

**Interface-driven extensibility.** `server.Stater`, `middleware.HTTPMetricsProvider`, `oauth2.Provider`, `oauth2.SessionStore` — swap out any backend without touching duck internals.

**Standard types, zero lock-in.** `httpclient.New` returns `*http.Client`. `log.New` returns `*slog.Logger`. You never carry a duck-specific wrapper through your codebase.

**Context as the carrier.** Request-scoped data — log attributes, JWT claims, OAuth2 sessions — lives in `context.Context`. Every function that needs it accepts `context.Context` as its first argument and propagates it to all downstream calls.

**No magic.** Duck does not use code generation, init functions, or global state. What you configure is what runs.

---

## Requirements

|               | Version     |
|---------------|-------------|
| Go            | **1.26+**   |
| golangci-lint | **v2.10.1** |

### Dependencies

Duck's sub-packages pull in their own dependencies independently. The table below lists the significant ones:

| Dependency                            | Version | Required by                 |
|---------------------------------------|---------|-----------------------------|
| `github.com/go-chi/chi/v5`            | v5      | `server`, `oauth2`          |
| `github.com/go-jose/go-jose/v4`       | v4      | `jwt`                       |
| `golang.org/x/oauth2`                 | latest  | `oauth2`                    |
| `github.com/redis/go-redis/v9`        | v9      | `oauth2/store` *(optional)* |
| `github.com/prometheus/client_golang` | latest  | `metrics` *(optional)*      |

---

## Code review

Pull requests on this project are reviewed with [CodeRabbit](https://coderabbit.ai), an AI-powered code review tool. CodeRabbit provides automated reviews on every PR — catching logic issues, suggesting improvements, and enforcing consistency across the codebase.

---

## License

MIT — see [LICENSE](./LICENSE).