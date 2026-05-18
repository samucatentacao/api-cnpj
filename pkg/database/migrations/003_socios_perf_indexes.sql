-- Índices para busca rápida por CPF mascarado (igualdade) e nome de sócio (trigram).
-- Em produção: CREATE INDEX CONCURRENTLY (fora de transação).

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_socios_cpf_mascarado
    ON cnpj.socios (cnpj_cpf_do_socio);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_socios_cnpj_basico
    ON cnpj.socios (cnpj_basico);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_socios_nome_socio_trgm
    ON cnpj.socios USING gin (nome_socio cnpj.gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_estab_cnpj_basico
    ON cnpj.estabelecimentos (cnpj_basico);
