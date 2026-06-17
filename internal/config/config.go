// Package config carrega a configuração do servidor a partir do ambiente
// (12-factor). Valores seguros por padrão; DevMode desligado em produção.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr    string // endereço de escuta (":8080")
	DatabaseURL string // DSN do Postgres (obrigatório)
	RedisURL    string // URL do Redis

	DevMode bool // habilita o client de teste em "/" e o fast-forward (NUNCA em produção)

	DBMaxConns int32 // tamanho máximo do pool do Postgres
	DBMinConns int32 // conexões ociosas mantidas

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	MaxBodyBytes int64    // limite do corpo das requisições
	CORSOrigins  []string // origens permitidas ("*" libera todas)

	// Autenticação (contas vinculadas à Steam).
	JWTSecret      string        // segredo HMAC dos tokens (obrigatório em produção)
	TokenTTL       time.Duration // validade do token de sessão
	SteamWebAPIKey string        // chave Web API da Steam (valida tickets em produção)
	SteamAppID     string        // AppID da Steam

	// Marketplace.
	MarketFeeBps int // taxa do mercado em basis points (1000 = 10%)
}

// Load lê e valida a configuração do ambiente.
func Load() (Config, error) {
	c := Config{
		HTTPAddr:          env("HTTP_ADDR", ":8080"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		RedisURL:          env("REDIS_URL", "redis://localhost:6379"),
		DevMode:           envBool("DEV_MODE", false),
		DBMaxConns:        int32(envInt("DB_MAX_CONNS", 25)),
		DBMinConns:        int32(envInt("DB_MIN_CONNS", 2)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       envDur("HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      envDur("HTTP_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:       envDur("HTTP_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout:   envDur("SHUTDOWN_TIMEOUT", 15*time.Second),
		MaxBodyBytes:      int64(envInt("HTTP_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:       splitCSV(env("CORS_ORIGINS", "*")),
		JWTSecret:         os.Getenv("JWT_SECRET"),
		TokenTTL:          envDur("TOKEN_TTL", 720*time.Hour), // 30 dias
		SteamWebAPIKey:    os.Getenv("STEAM_WEB_API_KEY"),
		SteamAppID:        os.Getenv("STEAM_APP_ID"),
		MarketFeeBps:      envInt("MARKET_FEE_BPS", 1000), // 10%
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL é obrigatória")
	}
	if c.DBMaxConns < 1 {
		return c, fmt.Errorf("DB_MAX_CONNS deve ser >= 1")
	}
	if c.JWTSecret == "" {
		if !c.DevMode {
			return c, fmt.Errorf("JWT_SECRET é obrigatório em produção")
		}
		c.JWTSecret = "dev-insecure-secret-change-me" // só em DevMode
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return def
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
