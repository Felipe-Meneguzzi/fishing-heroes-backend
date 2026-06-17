// Package repo — camada de repositório (Postgres/Redis). Converte row ↔ domínio.
// Sem regra de negócio: orquestração e fórmulas vivem em usecase/domain.
package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// PoolOptions — parâmetros de dimensionamento do pool do Postgres.
type PoolOptions struct {
	MaxConns int32
	MinConns int32
}

// NewPool abre o pool do Postgres dimensionado para concorrência alta,
// tolerando o serviço ainda subindo (docker compose / orquestrador).
func NewPool(ctx context.Context, dsn string, opt PoolOptions) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DSN inválida: %w", err)
	}
	if opt.MaxConns > 0 {
		cfg.MaxConns = opt.MaxConns
	}
	if opt.MinConns > 0 {
		cfg.MinConns = opt.MinConns
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	var lastErr error
	for i := 0; i < 30; i++ {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				return pool, nil
			} else {
				lastErr = pingErr
				pool.Close()
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("não conectou no Postgres: %w", lastErr)
}

// NewRedis abre o cliente Redis (cache quente; não é fonte da verdade).
func NewRedis(ctx context.Context, addr string) (*redis.Client, error) {
	opt, err := redis.ParseURL(addr)
	if err != nil {
		// aceita "host:porta" simples além de "redis://..."
		opt = &redis.Options{Addr: addr}
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("não conectou no Redis: %w", err)
	}
	return c, nil
}

// Ping verifica a saúde das dependências (readiness probe).
func Ping(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client) error {
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if rdb != nil {
		if err := rdb.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("redis: %w", err)
		}
	}
	return nil
}
