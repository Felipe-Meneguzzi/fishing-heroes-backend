// api — servidor HTTP de produção do Fishing Heroes.
//
// Conecta Postgres (fonte da verdade) e Redis (cache), carrega os templates na
// RAM no boot, sobe a API REST e encerra graciosamente em SIGTERM/SIGINT.
// Configuração por ambiente — ver internal/config.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fishingheroes/internal/auth"
	"fishingheroes/internal/config"
	"fishingheroes/internal/domain"
	"fishingheroes/internal/httpapi"
	"fishingheroes/internal/repo"
	"fishingheroes/internal/usecase"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuração inválida", "err", err)
		os.Exit(1)
	}
	setupLogger(cfg.DevMode)

	if err := run(cfg); err != nil {
		slog.Error("encerrando com erro", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config) error {
	// Encerramento gracioso: cancela em SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bootCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	pool, err := repo.NewPool(bootCtx, cfg.DatabaseURL, repo.PoolOptions{MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns})
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("postgres conectado", "max_conns", cfg.DBMaxConns)

	rdb, err := repo.NewRedis(bootCtx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()
	slog.Info("redis conectado")

	templates, err := repo.LoadTemplates(bootCtx, pool)
	if err != nil {
		return err
	}
	slog.Info("templates carregados",
		"mundos", len(templates.Worlds), "localizacoes", len(templates.Locations),
		"peixes", len(templates.Fish), "classes", len(templates.Classes))

	// Autenticação: verificador de ticket Steam + emissor de tokens.
	steam, err := steamVerifier(cfg)
	if err != nil {
		return err
	}
	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.TokenTTL)

	engine := domain.NewEngine(domain.DefaultConfig())
	svc := usecase.New(usecase.Deps{
		Pool: pool, Redis: rdb, Templates: templates, Engine: engine,
		Tokens: tokens, Steam: steam, MarketFeeBps: cfg.MarketFeeBps,
	})
	handler := httpapi.New(svc, httpapi.Options{
		DevMode:      cfg.DevMode,
		CORSOrigins:  cfg.CORSOrigins,
		MaxBodyBytes: cfg.MaxBodyBytes,
	}).Routes()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("Fishing Heroes API ouvindo", "addr", cfg.HTTPAddr, "dev_mode", cfg.DevMode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("sinal recebido, encerrando…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// steamVerifier escolhe a validação de ticket: Web API em produção, ou o
// verificador DEV (ticket = SteamID) quando DEV_MODE está ligado.
func steamVerifier(cfg config.Config) (auth.SteamVerifier, error) {
	if cfg.SteamWebAPIKey != "" && cfg.SteamAppID != "" {
		slog.Info("steam: verificador Web API")
		return auth.NewWebAPISteamVerifier(cfg.SteamWebAPIKey, cfg.SteamAppID), nil
	}
	if cfg.DevMode {
		slog.Warn("steam: verificador DEV (ticket = SteamID) — não use em produção")
		return auth.DevSteamVerifier{}, nil
	}
	return nil, fmt.Errorf("STEAM_WEB_API_KEY e STEAM_APP_ID são obrigatórios em produção")
}

func setupLogger(dev bool) {
	var h slog.Handler
	if dev {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	slog.SetDefault(slog.New(h))
}
