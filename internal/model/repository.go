package model

import "context"

// EmpresaRepository define o contrato de acesso a dados para empresas.
// A implementação concreta pode ser Postgres, MongoDB ou ambos via MultiRepository.
type EmpresaRepository interface {
	// Search realiza busca combinada por CNPJ, nome e/ou CPF de sócio.
	// Todos os parâmetros de SearchFilter são opcionais e combináveis.
	Search(ctx context.Context, filter SearchFilter) ([]*EmpresaResult, int, error)

	// GetByCNPJ busca uma empresa pelo CNPJ completo (14 dígitos).
	GetByCNPJ(ctx context.Context, cnpj string) (*EmpresaResult, error)
}

// CacheRepository define o contrato para o cache (Redis).
type CacheRepository interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttlSeconds int) error
	Delete(ctx context.Context, key string) error
}
