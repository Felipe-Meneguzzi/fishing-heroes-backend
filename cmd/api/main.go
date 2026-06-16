// api — servidor HTTP de produção do Fishing Heroes.
//
// Conecta Postgres (fonte da verdade) e Redis (cache), carrega os templates na
// RAM no boot e sobe a API REST. Config por variáveis de ambiente:
//
//	DATABASE_URL  postgres://user:pass@host:5432/db
//	REDIS_URL     redis://host:6379  (ou "host:6379")
//	HTTP_ADDR     :8080 (padrão)
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"fishingheroes/internal/domain"
	"fishingheroes/internal/httpapi"
	"fishingheroes/internal/repo"
	"fishingheroes/internal/usecase"
)

func main() {
	dsn := mustEnv("DATABASE_URL")
	redisURL := env("REDIS_URL", "redis://localhost:6379")
	addr := env("HTTP_ADDR", ":8080")

	bootCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := repo.NewPool(bootCtx, dsn)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()
	log.Println("postgres conectado")

	rdb, err := repo.NewRedis(bootCtx, redisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()
	log.Println("redis conectado")

	templates, err := repo.LoadTemplates(bootCtx, pool)
	if err != nil {
		log.Fatalf("carregando templates: %v", err)
	}
	log.Printf("templates carregados: %d mundos, %d localizações, %d peixes, %d classes",
		len(templates.Worlds), len(templates.Locations), len(templates.Fish), len(templates.Classes))

	engine := domain.NewEngine(domain.DefaultConfig())
	svc := usecase.New(pool, templates, engine)
	handler := httpapi.New(svc).Routes()

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Fishing Heroes API → http://localhost%s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("variável de ambiente obrigatória não definida: %s", key)
	}
	return v
}
