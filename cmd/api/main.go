package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"dataws_api/internal/handler"
	"dataws_api/internal/model"
	repomulti "dataws_api/internal/repository/multi"
	repopg "dataws_api/internal/repository/postgres"
	repocache "dataws_api/internal/repository/redis"
	"dataws_api/internal/service"
	"dataws_api/pkg/config"
	"dataws_api/pkg/database"
)

func main() {
	cfg := config.Load()

	log.Printf("Backends ativos: [%s]  primário: %s",
		strings.Join(cfg.DBBackends, ", "), cfg.PrimaryBackend())

	// ─── Cache (Redis — opcional) ────────────────────────────────────────────────
	var cacheRepo model.CacheRepository
	redisClient, err := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Printf("AVISO: Redis indisponível (%v) – usando cache nulo", err)
		cacheRepo = &nullCache{}
	} else {
		cacheRepo = repocache.NewCacheRepository(redisClient)
	}

	// ─── Construção dos backends na ordem declarada em DB_BACKENDS ───────────────
	repos, cleanup := buildRepositories(cfg)
	defer cleanup()

	empresaRepo, err := repomulti.NewMultiRepository(repos...)
	if err != nil {
		log.Fatalf("Falha ao criar MultiRepository: %v", err)
	}

	// ─── Injeção de Dependência ──────────────────────────────────────────────────
	empresaSvc := service.NewEmpresaService(empresaRepo, cacheRepo)
	empresaHandler := handler.NewEmpresaHandler(empresaSvc)

	// ─── Servidor HTTP (Gin) ─────────────────────────────────────────────────────
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"backends":  cfg.DBBackends,
			"primary":   cfg.PrimaryBackend(),
			"timestamp": time.Now().UTC(),
		})
	})

	api := router.Group("/api/v1")
	empresaHandler.RegisterRoutes(api)

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ─── Graceful Shutdown ───────────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Servidor iniciado na porta %s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Falha ao iniciar servidor: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Desligando servidor...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Erro no shutdown: %v", err)
	}
	log.Println("Servidor encerrado.")
}

// buildRepositories instancia cada backend na ordem de DB_BACKENDS.
// Atualmente apenas Postgres está implementado para o schema RFB.
// MongoDB pode ser adicionado como réplica de leitura no futuro.
func buildRepositories(cfg *config.Config) ([]model.EmpresaRepository, func()) {
	var repos []model.EmpresaRepository
	var cleanups []func()

	for _, backend := range cfg.DBBackends {
		switch backend {
		case "postgres":
			pool, err := database.NewPostgresPool(cfg.PostgresURL)
			if err != nil {
				log.Fatalf("Falha ao conectar ao Postgres: %v", err)
			}
			repos = append(repos, repopg.NewEmpresaRepository(pool))
			cleanups = append(cleanups, pool.Close)
			log.Println("Backend Postgres inicializado")

		case "mongodb":
			// MongoDB pode ser adicionado aqui quando a implementação
			// do EmpresaRepository para Mongo estiver pronta.
			log.Println("AVISO: MongoDB não implementado para o schema RFB — ignorado")

		default:
			log.Fatalf("Backend desconhecido: %q. Use 'postgres' ou 'mongodb'", backend)
		}
	}

	if len(repos) == 0 {
		log.Fatal("Nenhum backend de persistência foi inicializado com sucesso")
	}

	return repos, func() {
		for _, fn := range cleanups {
			fn()
		}
	}
}

// nullCache é um cache nulo (no-op) usado quando o Redis está indisponível.
type nullCache struct{}

func (n *nullCache) Get(_ context.Context, _ string) (string, error) {
	return "", model.ErrCachemiss
}
func (n *nullCache) Set(_ context.Context, _ string, _ string, _ int) error { return nil }
func (n *nullCache) Delete(_ context.Context, _ string) error               { return nil }
