package multi

import (
	"context"
	"fmt"
	"sync"

	"dataws_api/internal/model"
)

// multiRepository implementa EmpresaRepository fazendo fan-out de escritas
// (quando houver) e lendo sempre do backend primário (índice 0).
// Nesta versão a API é somente leitura (dados da RFB), então apenas as
// leituras são implementadas, mas a estrutura está pronta para extensão.
type multiRepository struct {
	primary  model.EmpresaRepository
	replicas []model.EmpresaRepository
}

// NewMultiRepository cria um repositório que agrega múltiplos backends.
// O primeiro elemento da fatia é tratado como primário para leituras.
func NewMultiRepository(repos ...model.EmpresaRepository) (model.EmpresaRepository, error) {
	if len(repos) == 0 {
		return nil, fmt.Errorf("multi: ao menos um repositório é obrigatório")
	}
	return &multiRepository{
		primary:  repos[0],
		replicas: repos,
	}, nil
}

// ─── Leituras: apenas do primário ─────────────────────────────────────────────

func (m *multiRepository) GetByCNPJ(ctx context.Context, cnpj string) (*model.EmpresaResult, error) {
	return m.primary.GetByCNPJ(ctx, cnpj)
}

func (m *multiRepository) Search(ctx context.Context, filter model.SearchFilter) ([]*model.EmpresaResult, int, error) {
	return m.primary.Search(ctx, filter)
}

// ─── fan-out helper (para uso futuro em operações de escrita) ─────────────────

func (m *multiRepository) fanOut(ctx context.Context, op func(model.EmpresaRepository) error) error {
	errs := make([]error, len(m.replicas))
	var wg sync.WaitGroup
	for i, repo := range m.replicas {
		wg.Add(1)
		go func(idx int, r model.EmpresaRepository) {
			defer wg.Done()
			errs[idx] = op(r)
		}(i, repo)
	}
	wg.Wait()
	return joinErrors(errs)
}

func joinErrors(errs []error) error {
	var msgs []string
	for _, e := range errs {
		if e != nil {
			msgs = append(msgs, e.Error())
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	combined := ""
	for i, m := range msgs {
		if i > 0 {
			combined += "; "
		}
		combined += m
	}
	return fmt.Errorf("multi: %s", combined)
}
