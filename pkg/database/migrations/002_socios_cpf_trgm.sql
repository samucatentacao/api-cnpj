-- Índice para busca parcial por CPF/CNPJ de sócio (ILIKE '%247464%').
-- Executar com search_path incluindo cnpj (pg_trgm instalado em cnpj).
-- Pode demorar em bases grandes; use CONCURRENTLY em produção.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_socios_cpf_trgm
    ON cnpj.socios USING gin (cnpj_cpf_do_socio cnpj.gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_socios_repr_legal_trgm
    ON cnpj.socios USING gin (representante_legal cnpj.gin_trgm_ops);
