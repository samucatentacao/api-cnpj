package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"dataws_api/internal/model"
)

const cacheTTL = 3600 // 1 hora

// EmpresaService contém a lógica de negócio para consulta de empresas.
type EmpresaService struct {
	repo  model.EmpresaRepository
	cache model.CacheRepository
}

func NewEmpresaService(repo model.EmpresaRepository, cache model.CacheRepository) *EmpresaService {
	return &EmpresaService{repo: repo, cache: cache}
}

// GetByCNPJ busca uma empresa pelo CNPJ completo (14 dígitos, com ou sem máscara).
func (s *EmpresaService) GetByCNPJ(ctx context.Context, cnpj string) (*model.EmpresaResult, error) {
	cnpj = sanitizeCNPJ(cnpj)
	if err := validateCNPJ(cnpj); err != nil {
		return nil, err
	}

	cacheKey := "cnpj:" + cnpj
	if cached, err := s.cache.Get(ctx, cacheKey); err == nil {
		var result model.EmpresaResult
		if err := json.Unmarshal([]byte(cached), &result); err == nil {
			return &result, nil
		}
	}

	result, err := s.repo.GetByCNPJ(ctx, cnpj)
	if err != nil {
		return nil, err
	}

	if data, err := json.Marshal(result); err == nil {
		_ = s.cache.Set(ctx, cacheKey, string(data), cacheTTL)
	}
	return result, nil
}

// GetRandom retorna um estabelecimento aleatório. Não usa cache para cada chamada ser independente.
func (s *EmpresaService) GetRandom(ctx context.Context) (*model.EmpresaResult, error) {
	return s.repo.GetRandom(ctx)
}

// Search realiza busca combinada. Ao menos um dos campos (CNPJ, Nome, CPF) deve ser informado.
func (s *EmpresaService) Search(ctx context.Context, f model.SearchFilter) ([]*model.EmpresaResult, int, error) {
	// Sanitiza entradas
	f.CNPJ = sanitizeCNPJ(f.CNPJ)
	f.CPF = sanitizeCPF(f.CPF)
	f.Nome = strings.TrimSpace(f.Nome)
	f.NomeSocio = strings.TrimSpace(f.NomeSocio)

	if f.CNPJ == "" && f.Nome == "" && f.CPF == "" && f.NomeSocio == "" {
		return nil, 0, model.ErrNoFilter
	}

	if f.NomeSocio != "" && len(f.NomeSocio) < 3 {
		return nil, 0, model.ErrNomeSocioShort
	}

	// CPF parcial: na base da Receita o campo vem mascarado (ex: ***247464**).
	// Exige ao menos 4 dígitos para não varrer a tabela inteira.
	if f.CPF != "" && len(f.CPF) != 11 && len(f.CPF) < 4 {
		return nil, 0, model.ErrCPFTooShort
	}

	// Paginação segura
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}

	// Cache de busca (chave derivada do hash do filtro)
	cacheKey := "search:" + filterHash(f)
	if cached, err := s.cache.Get(ctx, cacheKey); err == nil {
		var payload searchPayload
		if err := json.Unmarshal([]byte(cached), &payload); err == nil {
			return payload.Data, payload.Total, nil
		}
	}

	results, total, err := s.repo.Search(ctx, f)
	if err != nil {
		return nil, 0, err
	}

	if data, err := json.Marshal(searchPayload{Data: results, Total: total}); err == nil {
		_ = s.cache.Set(ctx, cacheKey, string(data), 300) // 5 min para buscas
	}

	return results, total, nil
}

// ─── Helpers internos ─────────────────────────────────────────────────────────

type searchPayload struct {
	Data  []*model.EmpresaResult `json:"data"`
	Total int                    `json:"total"`
}

func filterHash(f model.SearchFilter) string {
	raw := fmt.Sprintf("%v", f)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h[:8])
}

func sanitizeCNPJ(cnpj string) string {
	cnpj = strings.ReplaceAll(cnpj, ".", "")
	cnpj = strings.ReplaceAll(cnpj, "/", "")
	cnpj = strings.ReplaceAll(cnpj, "-", "")
	return strings.TrimSpace(cnpj)
}

func sanitizeCPF(cpf string) string {
	cpf = strings.ReplaceAll(cpf, ".", "")
	cpf = strings.ReplaceAll(cpf, "-", "")
	return strings.TrimSpace(cpf)
}

func validateCNPJ(cnpj string) error {
	if cnpj == "" {
		return nil
	}
	if len(cnpj) != 14 {
		return model.ErrInvalidCNPJ
	}
	re := regexp.MustCompile(`^\d{14}$`)
	if !re.MatchString(cnpj) {
		return model.ErrInvalidCNPJ
	}
	if err := checkCNPJDigits(cnpj); err != nil {
		return model.ErrInvalidCNPJ
	}
	return nil
}

func checkCNPJDigits(cnpj string) error {
	calc := func(cnpj string, length int) int {
		sum, pos := 0, length-7
		for i := length; i >= 1; i-- {
			sum += int(cnpj[length-i]-'0') * pos
			pos--
			if pos < 2 {
				pos = 9
			}
		}
		if rem := sum % 11; rem < 2 {
			return 0
		} else {
			return 11 - rem
		}
	}
	if int(cnpj[12]-'0') != calc(cnpj, 12) || int(cnpj[13]-'0') != calc(cnpj, 13) {
		return fmt.Errorf("dígitos inválidos")
	}
	allSame := true
	for i := 1; i < 14; i++ {
		if cnpj[i] != cnpj[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return fmt.Errorf("cnpj sequencial inválido")
	}
	return nil
}
