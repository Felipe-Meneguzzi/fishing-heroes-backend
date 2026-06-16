// Package usecase orquestra domínio + repositórios. Sem regra matemática aqui
// (delega ao domain) e sem SQL (delega ao repo).
package usecase

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"fishingheroes/internal/domain"
	"fishingheroes/internal/repo"
)

// Service expõe os casos de uso do backend.
type Service struct {
	Pool      *pgxpool.Pool
	Templates *repo.Templates
	Engine    *domain.Engine
}

func New(pool *pgxpool.Pool, t *repo.Templates, e *domain.Engine) *Service {
	return &Service{Pool: pool, Templates: t, Engine: e}
}

// NewPlayer cria um jogador com o kit inicial.
func (s *Service) NewPlayer(ctx context.Context, name, class string) (*domain.Player, error) {
	if name == "" {
		return nil, fmt.Errorf("nome obrigatório")
	}
	id, err := repo.CreatePlayer(ctx, s.Pool, s.Templates, name, class)
	if err != nil {
		return nil, err
	}
	return repo.GetPlayer(ctx, s.Pool, id)
}

// GetPlayer carrega o estado do jogador.
func (s *Service) GetPlayer(ctx context.Context, id string) (*domain.Player, error) {
	return repo.GetPlayer(ctx, s.Pool, id)
}
