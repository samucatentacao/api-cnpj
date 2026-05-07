package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// Servidor
	ServerPort string

	// Postgres
	PostgresURL string

	// MongoDB
	MongoURL string
	MongoDB  string

	// Redis
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// Backends ativos: lista separada por vírgula, ex: "postgres,mongodb"
	// O primeiro da lista é o primário (leituras).
	// Todos recebem escritas em paralelo (fan-out).
	DBBackends []string
}

// UsePostgres informa se Postgres está habilitado.
func (c *Config) UsePostgres() bool { return contains(c.DBBackends, "postgres") }

// UseMongoDB informa se MongoDB está habilitado.
func (c *Config) UseMongoDB() bool { return contains(c.DBBackends, "mongodb") }

// PrimaryBackend retorna o nome do backend primário (leituras).
func (c *Config) PrimaryBackend() string {
	if len(c.DBBackends) == 0 {
		return "postgres"
	}
	return c.DBBackends[0]
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Arquivo .env não encontrado, usando variáveis de ambiente do sistema")
	}

	raw := getEnv("DB_BACKENDS", "postgres")
	backends := parseList(raw)

	return &Config{
		ServerPort:    getEnv("SERVER_PORT", "8080"),
		PostgresURL:   getEnv("POSTGRES_URL", "postgres://postgres:postgres@localhost:5432/dataws?sslmode=disable"),
		MongoURL:      getEnv("MONGO_URL", "mongodb://localhost:27017"),
		MongoDB:       getEnv("MONGO_DB", "dataws"),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       0,
		DBBackends:    backends,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseList transforma "postgres, mongodb" em ["postgres","mongodb"].
func parseList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(strings.ToLower(p)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
