// Package usecase orquestra domínio + repositórios. Sem regra matemática aqui
// (delega ao domain) e sem SQL (delega ao repo).
package usecase

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"fishingheroes/internal/auth"
	"fishingheroes/internal/domain"
	"fishingheroes/internal/repo"
)

// Service expõe os casos de uso do backend.
type Service struct {
	Pool         *pgxpool.Pool
	Redis        *redis.Client
	Templates    *repo.Templates
	Engine       *domain.Engine
	Tokens       *auth.TokenManager
	Steam        auth.SteamVerifier
	MarketFeeBps int
}

// Deps — dependências do Service.
type Deps struct {
	Pool         *pgxpool.Pool
	Redis        *redis.Client
	Templates    *repo.Templates
	Engine       *domain.Engine
	Tokens       *auth.TokenManager
	Steam        auth.SteamVerifier
	MarketFeeBps int
}

func New(d Deps) *Service {
	return &Service{
		Pool: d.Pool, Redis: d.Redis, Templates: d.Templates, Engine: d.Engine,
		Tokens: d.Tokens, Steam: d.Steam, MarketFeeBps: d.MarketFeeBps,
	}
}

// Ready verifica as dependências (Postgres/Redis) para a readiness probe.
func (s *Service) Ready(ctx context.Context) error {
	return repo.Ping(ctx, s.Pool, s.Redis)
}

// GetPlayer carrega o estado completo do jogador.
func (s *Service) GetPlayer(ctx context.Context, id string) (*domain.Player, error) {
	return repo.GetPlayer(ctx, s.Pool, id)
}
