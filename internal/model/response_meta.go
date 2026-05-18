package model

import "time"

// ConsultaMeta acompanha data/hora e duração de cada requisição à API.
type ConsultaMeta struct {
	ConsultadoEm      string `json:"consultado_em"`
	TempoConsultaMs   int64  `json:"tempo_consulta_ms"`
}

func NewConsultaMeta(start time.Time) ConsultaMeta {
	return ConsultaMeta{
		ConsultadoEm:    start.UTC().Format(time.RFC3339),
		TempoConsultaMs: time.Since(start).Milliseconds(),
	}
}

// EmpresaDetailResponse envelope para consulta unitária (CNPJ ou random).
type EmpresaDetailResponse struct {
	ConsultaMeta
	Data *EmpresaResult `json:"data"`
}

// EmpresaSearchResponse envelope para busca paginada.
type EmpresaSearchResponse struct {
	ConsultaMeta
	Data  []*EmpresaResult `json:"data"`
	Total int              `json:"total"`
	Page  int              `json:"page"`
	Limit int              `json:"limit"`
	Pages int              `json:"pages"`
}
