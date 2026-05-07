package database

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPostgresPool(connStr string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("falha ao parsear config postgres: %w", err)
	}

	// Define search_path para incluir o schema cnpj — necessário para o
	// operador gin_trgm_ops (pg_trgm instalado no schema cnpj) funcionar nos índices.
	cfg.ConnConfig.RuntimeParams["search_path"] = "cnpj"

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar pool postgres: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("falha ao conectar ao postgres: %w", err)
	}

	log.Println("Postgres conectado com sucesso")
	return pool, nil
}
