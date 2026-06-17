package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertAccountBySteam cria ou atualiza a conta vinculada ao SteamID.
// Devolve o id e se a conta acabou de ser criada (xmax=0 ⇒ INSERT).
func UpsertAccountBySteam(ctx context.Context, pool *pgxpool.Pool, steamID, displayName string) (accountID string, isNew bool, err error) {
	err = pool.QueryRow(ctx, `INSERT INTO accounts (steam_id, display_name, last_login_at)
		VALUES ($1, $2, now())
		ON CONFLICT (steam_id) DO UPDATE
		SET last_login_at = now(),
		    display_name = CASE WHEN $2 <> '' THEN $2 ELSE accounts.display_name END
		RETURNING id, (xmax = 0) AS is_new`, steamID, displayName).Scan(&accountID, &isNew)
	return accountID, isNew, err
}

// GetPlayerByAccount devolve o jogador da conta (ErrPlayerNotFound se não houver).
func GetPlayerByAccount(ctx context.Context, pool *pgxpool.Pool, accountID string) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `SELECT id FROM players WHERE account_id = $1`, accountID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPlayerNotFound
	}
	return id, err
}
