-- Migration: 001_create_cnpjs
-- Tabela principal para armazenar dados de CNPJ

CREATE TABLE IF NOT EXISTS cnpjs (
    cnpj                    CHAR(14)          PRIMARY KEY,
    razao_social            VARCHAR(255)      NOT NULL,
    nome_fantasia           VARCHAR(255),
    situacao_cadastral      VARCHAR(50),
    data_situacao_cadastral VARCHAR(20),
    cnae_fiscal             VARCHAR(10),
    cnae_fiscal_descricao   TEXT,
    natureza_juridica       VARCHAR(100),
    porte                   VARCHAR(50),
    capital_social          NUMERIC(18, 2)    DEFAULT 0,

    -- Endereço (desnormalizado para performance de leitura)
    logradouro              VARCHAR(255),
    numero                  VARCHAR(20),
    complemento             VARCHAR(100),
    bairro                  VARCHAR(100),
    municipio               VARCHAR(100),
    uf                      CHAR(2),
    cep                     VARCHAR(10),

    -- Contato
    telefone1               VARCHAR(20),
    telefone2               VARCHAR(20),
    email                   VARCHAR(150),

    created_at              TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ       NOT NULL DEFAULT NOW()
);

-- Índices para os filtros mais comuns
CREATE INDEX IF NOT EXISTS idx_cnpjs_uf            ON cnpjs(uf);
CREATE INDEX IF NOT EXISTS idx_cnpjs_municipio     ON cnpjs(municipio);
CREATE INDEX IF NOT EXISTS idx_cnpjs_situacao      ON cnpjs(situacao_cadastral);
CREATE INDEX IF NOT EXISTS idx_cnpjs_cnae          ON cnpjs(cnae_fiscal);
CREATE INDEX IF NOT EXISTS idx_cnpjs_razao_social  ON cnpjs(razao_social);
