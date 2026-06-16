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

// Store agrega os recursos de infraestrutura (fonte da verdade + cache).
type Store struct {
	Pool  *pgxpool.Pool
	Redis *redis.Client
}

// NewPool abre o pool do Postgres, tolerando o serviço ainda subindo (compose).
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DSN inválida: %w", err)
	}
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
