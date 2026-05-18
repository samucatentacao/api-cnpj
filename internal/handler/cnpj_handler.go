package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dataws_api/internal/model"
	"dataws_api/internal/service"
)

type EmpresaHandler struct {
	svc *service.EmpresaService
}

func NewEmpresaHandler(svc *service.EmpresaService) *EmpresaHandler {
	return &EmpresaHandler{svc: svc}
}

// RegisterRoutes registra as rotas da API de CNPJ.
func (h *EmpresaHandler) RegisterRoutes(r *gin.RouterGroup) {
	cnpjs := r.Group("/cnpjs")
	{
		// GET /api/v1/cnpjs/random — deve vir ANTES de /:cnpj
		cnpjs.GET("/random", h.GetRandom)

		// GET /api/v1/cnpjs/:cnpj  — busca exata por CNPJ completo (14 dígitos)
		cnpjs.GET("/:cnpj", h.GetByCNPJ)

		// GET /api/v1/cnpjs?cnpj=...&nome=...&cpf=...&uf=...&...
		cnpjs.GET("", h.Search)
	}
}

// GetByCNPJ godoc
//
//	@Summary     Consulta empresa por CNPJ completo
//	@Description Retorna dados completos da empresa: dados cadastrais, estabelecimento, sócios e Simples Nacional
//	@Tags        cnpj
//	@Param       cnpj path string true "CNPJ (14 dígitos, com ou sem máscara)"
//	@Success     200 {object} model.EmpresaResult
//	@Failure     404 {object} map[string]string
//	@Failure     422 {object} map[string]string
//	@Router      /cnpjs/{cnpj} [get]
func (h *EmpresaHandler) GetByCNPJ(c *gin.Context) {
	cnpj := c.Param("cnpj")

	// Se o binário não tiver a rota /random registrada antes de /:cnpj, "random"
	// cai aqui e o validador de CNPJ falha — trata como aleatório.
	if strings.EqualFold(strings.TrimSpace(cnpj), "random") {
		h.GetRandom(c)
		return
	}

	result, err := h.svc.GetByCNPJ(c.Request.Context(), cnpj)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetRandom retorna um CNPJ (estabelecimento) aleatório da base, com o mesmo payload de GetByCNPJ.
func (h *EmpresaHandler) GetRandom(c *gin.Context) {
	result, err := h.svc.GetRandom(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Search godoc
//
//	@Summary     Busca empresas por múltiplos critérios
//	@Description Busca combinada por CNPJ, razão social/nome fantasia e/ou CPF de sócio.
//	             Ao menos um parâmetro de busca (cnpj, nome ou cpf) deve ser informado.
//	             Os demais parâmetros são filtros opcionais.
//	@Tags        cnpj
//	@Param       cnpj               query string false "CNPJ completo (14 dígitos) ou parcial (8 dígitos do bloco básico)"
//	@Param       nome               query string false "Razão social ou nome fantasia (busca parcial)"
//	@Param       cpf                query string false "CPF do sócio (cnpj.socios) — 6 dígitos visíveis ex: 247464 ou 11 dígitos completos"
//	@Param       uf                 query string false "Sigla do estado (ex: SP)"
//	@Param       municipio          query string false "Nome do município (busca parcial)"
//	@Param       situacao_cadastral query string false "Código da situação cadastral (ex: 02=ATIVA)"
//	@Param       cnae               query string false "Código CNAE principal (7 dígitos)"
//	@Param       porte              query string false "Código do porte (00,01,03,05)"
//	@Param       page               query int    false "Página (padrão: 1)"
//	@Param       limit              query int    false "Registros por página (padrão: 20, máx: 100)"
//	@Success     200 {object} SearchResponse
//	@Failure     400 {object} map[string]string
//	@Router      /cnpjs [get]
func (h *EmpresaHandler) Search(c *gin.Context) {
	filter := model.SearchFilter{
		CNPJ:              c.Query("cnpj"),
		Nome:              c.Query("nome"),
		CPF:               c.Query("cpf"),
		NomeSocio:         c.Query("nome_socio"),
		UF:                c.Query("uf"),
		Municipio:         c.Query("municipio"),
		SituacaoCadastral: c.Query("situacao_cadastral"),
		CNAE:              c.Query("cnae"),
		Porte:             c.Query("porte"),
		Page:              queryInt(c, "page", 1),
		Limit:             queryInt(c, "limit", 20),
	}

	results, total, err := h.svc.Search(c.Request.Context(), filter)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, SearchResponse{
		Data:  results,
		Total: total,
		Page:  filter.Page,
		Limit: filter.Limit,
		Pages: pages(total, filter.Limit),
	})
}

// SearchResponse é o envelope de resposta para buscas paginadas.
type SearchResponse struct {
	Data  []*model.EmpresaResult `json:"data"`
	Total int                    `json:"total"`
	Page  int                    `json:"page"`
	Limit int                    `json:"limit"`
	Pages int                    `json:"pages"`
}

func handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrInvalidCNPJ):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrNoFilter), errors.Is(err, model.ErrCPFTooShort), errors.Is(err, model.ErrNomeSocioShort):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrRandomSample):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		log.Printf("ERROR: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno do servidor", "detail": err.Error()})
	}
}

func queryInt(c *gin.Context, key string, fallback int) int {
	if v, err := strconv.Atoi(c.Query(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}

func pages(total, limit int) int {
	if limit <= 0 {
		return 0
	}
	p := total / limit
	if total%limit != 0 {
		p++
	}
	return p
}
