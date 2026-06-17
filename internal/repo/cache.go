package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache quente da sessão (Redis). O Postgres continua sendo a fonte da verdade;
// o Redis guarda a âncora + baseline para o caminho QUENTE (tick do WebSocket /
// preview) ler em velocidade de memória, sem bater no Postgres a cada ~1,5s.
//
// Consistência: tudo que muda a âncora ou o ouro/XP passa pelo use-case, que
// atualiza ou invalida esta chave. Em qualquer dúvida (claim concorrente, venda
// no mercado, recompensa offline) a chave é apagada e repovoada do Postgres.

const sessionCacheTTL = 2 * time.Hour

// HotSession — snapshot quente de uma sessão (o que o resolve precisa por tick).
type HotSession struct {
	Row      SessionRow     `json:"row"`
	Baseline PlayerBaseline `json:"baseline"`
}

func sessionKey(playerID string) string { return "sess:" + playerID }

// CacheGetSession lê o snapshot quente (ok=false em miss; nunca erra a chamada).
func CacheGetSession(ctx context.Context, rdb *redis.Client, playerID string) (*HotSession, bool) {
	if rdb == nil {
		return nil, false
	}
	b, err := rdb.Get(ctx, sessionKey(playerID)).Bytes()
	if err != nil {
		return nil, false // miss ou Redis indisponível → cai para o Postgres
	}
	var h HotSession
	if json.Unmarshal(b, &h) != nil {
		return nil, false
	}
	return &h, true
}

// CacheSetSession grava o snapshot quente com TTL.
func CacheSetSession(ctx context.Context, rdb *redis.Client, playerID string, h *HotSession) {
	if rdb == nil {
		return
	}
	if b, err := json.Marshal(h); err == nil {
		_ = rdb.Set(ctx, sessionKey(playerID), b, sessionCacheTTL).Err()
	}
}

// CacheDelSession invalida o snapshot quente (próxima leitura repovoa do PG).
func CacheDelSession(ctx context.Context, rdb *redis.Client, playerID string) {
	if rdb == nil {
		return
	}
	_ = rdb.Del(ctx, sessionKey(playerID)).Err()
}
