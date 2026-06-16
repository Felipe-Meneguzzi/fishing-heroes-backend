// migrate — aplicador de migrations simples (sem dependência externa).
//
// Lê os arquivos migrations/*.sql em ordem lexical e aplica os ainda não
// registrados na tabela schema_migrations, cada um numa transação. Idempotente:
// rodar de novo não reaplica o que já passou. Seed de templates entra como uma
// migration numerada (DB nova). Uso:
//
//	DATABASE_URL=postgres://... go run ./cmd/migrate
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL não definida")
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("DSN inválida: %v", err)
	}
	// Protocolo simples permite múltiplos statements por Exec (um arquivo inteiro).
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	conn, err := connectWithRetry(ctx, cfg)
	if err != nil {
		log.Fatalf("falha ao conectar no Postgres: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		log.Fatalf("não foi possível garantir schema_migrations: %v", err)
	}

	applied := map[string]bool{}
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		log.Fatalf("erro lendo schema_migrations: %v", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			log.Fatal(err)
		}
		applied[v] = true
	}
	rows.Close()

	// Aplica apenas os arquivos .up.sql (os .down.sql servem p/ rollback manual).
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		log.Fatal(err)
	}
	sort.Strings(files)

	pending := 0
	for _, f := range files {
		version := strings.TrimSuffix(filepath.Base(f), ".up.sql")
		if applied[version] {
			continue
		}
		sqlBytes, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("erro lendo %s: %v", f, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("erro aplicando %s: %v", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("erro registrando %s: %v", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("erro no commit de %s: %v", version, err)
		}
		log.Printf("aplicada: %s", version)
		pending++
	}

	if pending == 0 {
		log.Println("nenhuma migration pendente — banco já atualizado")
	} else {
		log.Printf("%d migration(s) aplicada(s)", pending)
	}
}

// connectWithRetry tolera o Postgres ainda subindo (útil no docker compose).
func connectWithRetry(ctx context.Context, cfg *pgx.ConnConfig) (*pgx.Conn, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err == nil {
			if pingErr := conn.Ping(ctx); pingErr == nil {
				return conn, nil
			} else {
				lastErr = pingErr
				_ = conn.Close(ctx)
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
	return nil, lastErr
}
