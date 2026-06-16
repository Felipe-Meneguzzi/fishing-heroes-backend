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
go run ./cmd/migrate      # schema + seed (idempotente)
go run ./cmd/api          # API em :8080

go test ./...             # testes (domínio é 100% testável sem I/O)
go run ./cmd/resolvedemo  # demo do motor Resolve (8h de pesca)
go run ./cmd/devserver    # visualizador HTML do motor em :8080
```

## Estrutura (Clean Architecture — dependências apontam para dentro)

| Camada | Pasta | Responsabilidade |
|---|---|---|
| Domínio | `internal/domain` | regras puras/determinísticas: motor `Resolve`, troféus, `Stats`, `Player`, skill tree, `BalanceConfig` |
| Use Cases | `internal/usecase` | orquestra domínio + repositórios (criar jogador, pescar+claim) |
| Repositórios | `internal/repo` | Postgres/Redis; carrega templates na RAM; transação de claim |
| Delivery | `internal/httpapi`, `cmd/api` | API REST |
| Ferramentas | `cmd/migrate` | aplicador de migrations + seed |

**Migrations** em `migrations/` no padrão `NNNNNN_<nome>.up.sql` + `.down.sql` (um objeto por arquivo, estilo neohabit): `000001`–`000026` criam o schema (uma tabela por arquivo) e `000027`–`000039` são os seeders do MVP, cada conteúdo em seu próprio arquivo (classes, peixes, spawn, mundos, etc.). O `cmd/migrate` aplica só os `.up.sql` ainda não registrados.

## Endpoints

| Método | Rota | Descrição |
|---|---|---|
| GET | `/health` | liveness |
| GET | `/api/worlds` | mundos e localizações |
| GET | `/api/templates` | catálogo (classes, peixes, equipamentos, iscas, runas, skills, pets) |
| POST | `/api/players` | cria jogador `{name, class}` + kit inicial |
| GET | `/api/players/{id}` | estado do jogador |
| POST | `/api/players/{id}/session/start?location=&autorepair=` | entra no local: congela a build e cria a `fishing_session` |
| GET | `/api/players/{id}/session` | estado da sessão ativa |
| POST | `/api/players/{id}/session/resolve?ff=` | tick: resolve o tempo decorrido (Idle) e persiste; `ff` = fast-forward de DEV |
| POST | `/api/players/{id}/session/stop` | sai do local (encerra a sessão) |

> **Fluxo de gameplay (Idle):** o cliente abre em `/`, cria o jogador, **entra num local**
> (`session/start`) e o personagem pesca em **tempo real** — o cliente faz `session/resolve`
> a cada ~2,5 s, resolvendo o tempo decorrido e persistindo venda→ouro, XP, materiais, troféus,
> runas e desgaste. O `ff` é só atalho de DEV para simular horas sem esperar.

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
