package usecase

import (
	"context"
	"errors"
	"math"
	"time"

	"fishingheroes/internal/domain"
	"fishingheroes/internal/repo"
)

// OfflineReward — recompensa de catch-up aplicada no login (modo desligado).
type OfflineReward struct {
	Gold  int64   `json:"gold"`
	XP    int64   `json:"xp"`
	Hours float64 `json:"hours"`
}

// LoginResult — retorno do login Steam.
type LoginResult struct {
	Token   string         `json:"token"`
	Player  *domain.Player `json:"player"`
	Offline *OfflineReward `json:"offline,omitempty"`
}

// LoginSteam valida o ticket Steam, garante a conta+jogador, aplica a recompensa
// offline (se houver) e emite o token de sessão. `name`/`class` só são usados na
// primeira vez (criação do jogador).
func (s *Service) LoginSteam(ctx context.Context, ticket, name, class string) (*LoginResult, error) {
	steamID, err := s.Steam.Verify(ctx, ticket)
	if err != nil {
		return nil, err
	}
	accountID, _, err := repo.UpsertAccountBySteam(ctx, s.Pool, steamID, name)
	if err != nil {
		return nil, err
	}

	playerID, err := repo.GetPlayerByAccount(ctx, s.Pool, accountID)
	if errors.Is(err, repo.ErrPlayerNotFound) {
		if class == "" {
			class = "bruiser"
		}
		if name == "" {
			name = "Pescador"
		}
		playerID, err = repo.CreatePlayer(ctx, s.Pool, s.Templates, accountID, name, class)
	}
	if err != nil {
		return nil, err
	}

	offline, err := s.applyOffline(ctx, playerID)
	if err != nil {
		return nil, err
	}

	token, err := s.Tokens.Issue(playerID, accountID, steamID)
	if err != nil {
		return nil, err
	}
	player, err := repo.GetPlayer(ctx, s.Pool, playerID)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Token: token, Player: player, Offline: offline}, nil
}

// applyOffline concede a recompensa de catch-up se o jogador NÃO tiver sessão
// ativa (sessão ativa = Idle, resolvida ao reabrir) e houver tempo desde o
// último logout. Ver GAMEPLAY §6.1.
func (s *Service) applyOffline(ctx context.Context, playerID string) (*OfflineReward, error) {
	if _, err := repo.GetSession(ctx, s.Pool, playerID); !errors.Is(err, repo.ErrNoSession) {
		return nil, nil // sessão ativa (ou erro de leitura) → sem catch-up offline
	}
	base, err := repo.GetPlayerBaseline(ctx, s.Pool, playerID)
	if err != nil {
		return nil, err
	}
	if base.LastLogout == nil {
		return nil, nil
	}
	loc, ok := s.Templates.Locations[base.HighestLocationID]
	if !ok {
		return nil, nil
	}
	away := time.Since(*base.LastLogout).Seconds()
	gold, xp := domain.OfflineReward(away, loc.GoldPerHour, loc.XPPerHour, s.Engine.Cfg)
	if gold == 0 && xp == 0 {
		return nil, nil
	}
	newLevel := domain.LevelForXP(base.XP+xp, s.Engine.Cfg)
	added := domain.SkillPointsForLevel(newLevel) - domain.SkillPointsForLevel(base.Level)
	if added < 0 {
		added = 0
	}
	if err := repo.ApplyOfflineReward(ctx, s.Pool, playerID, gold, xp, newLevel, added); err != nil {
		return nil, err
	}
	repo.CacheDelSession(ctx, s.Redis, playerID) // baseline mudou (insurance)
	hours := math.Min(away, s.Engine.Cfg.OfflineCapSeconds) / 3600
	return &OfflineReward{Gold: gold, XP: xp, Hours: hours}, nil
}
