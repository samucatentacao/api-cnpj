package model

import "errors"

var (
	ErrNotFound      = errors.New("registro não encontrado")
	ErrInvalidCNPJ   = errors.New("CNPJ inválido")
	ErrInvalidCPF    = errors.New("CPF inválido")
	ErrCachemiss     = errors.New("chave não encontrada no cache")
	ErrNoFilter      = errors.New("informe ao menos um parâmetro de busca: cnpj, nome ou cpf")
	ErrRandomSample  = errors.New("não foi possível obter um CNPJ aleatório neste momento")
	ErrCPFTooShort   = errors.New("informe ao menos 4 dígitos do CPF para busca parcial")
)
