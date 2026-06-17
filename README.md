# Fishing Heroes — Backend

Backend em **Go** do Idle Fishing RPG. *Server-authoritative*, resolução
determinística e preguiçosa (lazy) da pesca. Stack: **Go + PostgreSQL + Redis**.

Visão e regras: [`GAMEPLAY.md`](GAMEPLAY.md) · arquitetura: [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Como rodar (Docker)

```bash
cp .env.example .env
docker compose up --build
```

Sobe Postgres, Redis, aplica migrations + seed (serviço `migrate`, one-shot) e
inicia a API em `http://localhost:8080`.

```bash
curl localhost:8080/health
curl localhost:8080/api/worlds
# criar jogador e pescar 1h em Campos:
PID=$(curl -s -XPOST localhost:8080/api/players -d '{"name":"Ana","class":"trapper"}' | grep -o '"ID": *"[^"]*"' | head -1 | cut -d'"' -f4)
curl -XPOST "localhost:8080/api/players/$PID/fish?seconds=3600&location=1-1"
```

## Desenvolvimento local (sem Docker para a app)

```bash
docker compose up -d postgres redis
export DATABASE_URL="postgres://fishing:fishing@localhost:5432/fishingheroes?sslmode=disable"
export REDIS_URL="redis://localhost:6379"
export DEV_MODE=true      # habilita o client de teste em / e o fast-forward
go run ./cmd/migrate      # schema + seed (idempotente)
go run ./cmd/api          # API em :8080 (client de teste em / quando DEV_MODE=true)

go test ./...             # testes (domínio é 100% testável sem I/O)
```

## Estrutura (Clean Architecture — dependências apontam para dentro)

| Camada | Pasta | Responsabilidade |
|---|---|---|
| Domínio | `internal/domain` | regras puras/determinísticas: motor `Resolve`, troféus, `Stats`, `Player`, skill tree, `BalanceConfig` |
| Use Cases | `internal/usecase` | orquestra domínio + repositórios (criar jogador, pescar+claim) |
| Repositórios | `internal/repo` | Postgres/Redis; carrega templates na RAM; transação de claim |
| Delivery | `internal/httpapi`, `cmd/api` | API REST + WebSocket + middlewares (recover, log, CORS, limite de corpo) |
| Auth | `internal/auth` | tokens de sessão (JWT) + verificação de ticket Steam (prod) / dev |
| Config | `internal/config` | carrega/valida a configuração do ambiente (12-factor) |
| Ferramentas | `cmd/migrate` | aplicador de migrations + seed |

**Migrations** em `migrations/` no padrão `NNNNNN_<nome>.up.sql` + `.down.sql` (um objeto por arquivo, estilo neohabit): `000001`–`000026` criam o schema (uma tabela por arquivo) e `000027`–`000039` são os seeders do MVP, cada conteúdo em seu próprio arquivo (classes, peixes, spawn, mundos, etc.). O `cmd/migrate` aplica só os `.up.sql` ainda não registrados.

## Endpoints

Rotas autenticadas usam `Authorization: Bearer <token>` e operam sobre o jogador do token (`/api/me`).

| Método | Rota | Auth | Descrição |
|---|---|:--:|---|
| GET | `/health` · `/ready` | — | liveness / readiness (pinga PG+Redis) |
| GET | `/api/worlds` · `/api/templates` | — | mundos/localizações e catálogo |
| POST | `/api/auth/steam` | — | login `{ticket, name?, class?}` → `{token, player, offline?}` (cria conta+jogador na 1ª vez) |
| GET | `/api/market?type=` | — | navega anúncios ativos |
| GET | `/api/me` | ✓ | estado completo do jogador |
| POST | `/api/me/session/start?location=&autorepair=` | ✓ | entra no local: congela a build e cria a `fishing_session` |
| GET | `/api/me/session` | ✓ | **preview** (leitura pura, sem escrita) — estado acumulado desde o claim |
| POST | `/api/me/session/claim` | ✓ | **claim** — persiste o acumulado e avança a âncora |
| POST | `/api/me/session/stop` | ✓ | persiste o pendente, encerra a sessão e marca o logout |
| GET | `/api/me/market` | ✓ | meus anúncios ativos |
| POST | `/api/me/market/list` | ✓ | anuncia `{itemType, itemId, price}` (troféu/equipamento) |
| POST | `/api/me/market/buy` | ✓ | compra `{listingId}` |
| POST | `/api/me/market/cancel` | ✓ | cancela `{listingId}` |
| GET | `/ws?token=` | ✓ | **WebSocket**: stream de eventos + estado para animação (foreground) |

> **Autenticação (Steam):** as contas são vinculadas ao SteamID. Em produção, `POST /api/auth/steam`
> valida o *session ticket* via Steam Web API (`STEAM_WEB_API_KEY`/`STEAM_APP_ID`). Em `DEV_MODE=true`
> (sem chaves) o `ticket` é tratado como o próprio SteamID — o client de teste gera um id de dev por navegador.
>
> **Fluxo de gameplay (Idle):** entra num local (`session/start`); o personagem pesca em tempo real.
> O cliente abre o **WebSocket** para animar (eventos + estado) — leitura pura, recalculada da seed,
> **sem escrita por tick** — e o servidor persiste com **`claim`** periódico (e ao sair). Só o claim escreve
> no Postgres, sustentando milhares de jogadores. **Recompensa offline** é creditada no login (catch-up
> da melhor Localização × redução, até 8h) quando não há sessão ativa.
>
> Em `DEV_MODE=true` o backend serve o client de teste em `/` e aceita `?ff=<segundos>` (fast-forward).
> **Em produção `DEV_MODE` fica `false`.**

## Conteúdo do MVP (Mundo 1)

* **Mundo 1 — Floresta** · Localizações **1-1 Campos** e **1-2 Montanhas**.
* **Classes:** Brutamontes (`bruiser`), Estrategista (`trapper`).
* **Skill Tree em cruz:** núcleo genérico + 4 braços (Força, Sorte, Velocidade, Tensão).
* **Equipamentos iniciais:** Vara de Bambu, Molinete Simples, Linha de Nylon.
* **Iscas:** Minhoca (consumível) e Colher Giratória (durável/reutilizável).
* **Peixes:** 3 comerciais que viram troféu por tamanho (Lambari, Traíra, Dourado),
  2 de crafting (Cascudo→Escama Dura, Bagre→Espinho Afiado) e 1 peixe-runa (Runa da Força).
* **Sistemas:** venda automática → ouro, XP/nível → pontos de skill, auto-reparo (dreno de ouro),
  troféus por tamanho (Comum ≥80% … Perfeito 100%) com peso/qualidade/local de captura. O peso é
  sorteado por **Distribuição Normal (Gauss)** — média no tamanho típico, troféus na cauda.
* **Contas Steam** (JWT) · **drops de equipamento** persistidos no stash · **recompensa offline** no login ·
  **WebSocket** de animação ao vivo · **marketplace** de troféus/equipamentos (anunciar/comprar, com taxa).
* **Caminho quente em Redis:** o tick do WebSocket lê a sessão de um cache quente (`sess:{playerId}`) —
  Postgres só escreve no claim (periódico). Sustenta milhares de jogadores assistindo ao vivo.
* **Animação (replay da linha do tempo):** o servidor resolve a verdade e envia os eventos com `atSec`; o
  cliente os **reproduz localmente no instante certo** e interpola ouro/XP — fluido e imune a jitter de rede
  (a seed permanece no servidor; anti-cheat preservado). O **client de teste** (`DEV_MODE=true`, em `/`) faz
  login simples por nome e mostra essa visualização ao vivo.
