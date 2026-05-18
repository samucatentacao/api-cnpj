package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		cnpjs.GET("/random", h.GetRandom)
		cnpjs.GET("/:cnpj", h.GetByCNPJ)
		cnpjs.GET("", h.Search)
	}
}

func (h *EmpresaHandler) GetByCNPJ(c *gin.Context) {
	start := time.Now()
	cnpj := c.Param("cnpj")

	if strings.EqualFold(strings.TrimSpace(cnpj), "random") {
		h.GetRandom(c)
		return
	}

	result, err := h.svc.GetByCNPJ(c.Request.Context(), cnpj)
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.EmpresaDetailResponse{
		ConsultaMeta: model.NewConsultaMeta(start),
		Data:         result,
	})
}

func (h *EmpresaHandler) GetRandom(c *gin.Context) {
	start := time.Now()
	result, err := h.svc.GetRandom(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.EmpresaDetailResponse{
		ConsultaMeta: model.NewConsultaMeta(start),
		Data:         result,
	})
}

func (h *EmpresaHandler) Search(c *gin.Context) {
	start := time.Now()
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

	c.JSON(http.StatusOK, model.EmpresaSearchResponse{
		ConsultaMeta: model.NewConsultaMeta(start),
		Data:         results,
		Total:        total,
		Page:         filter.Page,
		Limit:        filter.Limit,
		Pages:        pages(total, filter.Limit),
	})
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
