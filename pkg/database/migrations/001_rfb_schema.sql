-- ============================================================
-- Schema: cnpj  (dados abertos da Receita Federal do Brasil)
-- Baseado no layout oficial das tabelas publicadas pela RFB:
--   empresas, estabelecimentos, socios, dados_simples
--   + tabelas de domínio (cnae, municipios, paises, etc.)
-- ============================================================

CREATE SCHEMA IF NOT EXISTS cnpj;

-- ─── Tabelas de domínio ────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS cnpj.naturezas_juridicas (
    codigo  CHAR(4)       PRIMARY KEY,
    descricao VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS cnpj.qualificacoes_socios (
    codigo  CHAR(2)       PRIMARY KEY,
    descricao VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS cnpj.municipios (
    codigo  CHAR(7)       PRIMARY KEY,
    descricao VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS cnpj.paises (
    codigo  CHAR(3)       PRIMARY KEY,
    descricao VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS cnpj.motivos (
    codigo  CHAR(2)       PRIMARY KEY,
    descricao VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS cnpj.cnaes (
    codigo  CHAR(7)       PRIMARY KEY,
    descricao TEXT        NOT NULL
);

-- ─── Tabela principal: empresas ────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS cnpj.empresas (
    cnpj_basico                CHAR(8)       PRIMARY KEY,
    razao_social               VARCHAR(255)  NOT NULL,
    natureza_juridica          CHAR(4),
    qualificacao_responsavel   CHAR(2),
    capital_social             NUMERIC(18,2) DEFAULT 0,
    porte                      CHAR(2),      -- 00=N/A 01=MICRO 03=PEQUENA 05=DEMAIS
    ente_federativo_responsavel VARCHAR(50)
);

CREATE INDEX IF NOT EXISTS idx_empresas_razao_social
    ON cnpj.empresas USING gin(to_tsvector('portuguese', razao_social));

-- ─── Tabela: estabelecimentos ──────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS cnpj.estabelecimentos (
    cnpj_basico              CHAR(8)      NOT NULL,
    cnpj_ordem               CHAR(4)      NOT NULL,
    cnpj_dv                  CHAR(2)      NOT NULL,
    identificador_matriz_fil CHAR(1),     -- 1=MATRIZ 2=FILIAL
    nome_fantasia            VARCHAR(255),
    situacao_cadastral       CHAR(2),
    data_situacao_cadastral  CHAR(8),
    motivo_situacao_cadastral CHAR(2),
    nome_cidade_exterior     VARCHAR(100),
    pais                     CHAR(3),
    data_inicio_atividade    CHAR(8),
    cnae_fiscal_principal    CHAR(7),
    cnae_fiscal_secundario   TEXT,
    tipo_logradouro          VARCHAR(20),
    logradouro               VARCHAR(255),
    numero                   VARCHAR(10),
    complemento              VARCHAR(100),
    bairro                   VARCHAR(100),
    cep                      CHAR(8),
    uf                       CHAR(2),
    municipio                CHAR(7),
    ddd1                     CHAR(4),
    telefone1                VARCHAR(15),
    ddd2                     CHAR(4),
    telefone2                VARCHAR(15),
    ddd_fax                  CHAR(4),
    fax                      VARCHAR(15),
    email                    VARCHAR(150),
    situacao_especial        VARCHAR(100),
    data_situacao_especial   CHAR(8),

    PRIMARY KEY (cnpj_basico, cnpj_ordem, cnpj_dv),
    FOREIGN KEY (cnpj_basico) REFERENCES cnpj.empresas(cnpj_basico)
);

CREATE INDEX IF NOT EXISTS idx_estab_cnpj_completo
    ON cnpj.estabelecimentos ((cnpj_basico || cnpj_ordem || cnpj_dv));

CREATE INDEX IF NOT EXISTS idx_estab_nome_fantasia
    ON cnpj.estabelecimentos USING gin(to_tsvector('portuguese', COALESCE(nome_fantasia, '')));

CREATE INDEX IF NOT EXISTS idx_estab_uf          ON cnpj.estabelecimentos(uf);
CREATE INDEX IF NOT EXISTS idx_estab_municipio   ON cnpj.estabelecimentos(municipio);
CREATE INDEX IF NOT EXISTS idx_estab_situacao    ON cnpj.estabelecimentos(situacao_cadastral);
CREATE INDEX IF NOT EXISTS idx_estab_cnae        ON cnpj.estabelecimentos(cnae_fiscal_principal);

-- ─── Tabela: socios ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS cnpj.socios (
    cnpj_basico                   CHAR(8)     NOT NULL,
    identificador_socio           CHAR(1),    -- 1=PJ 2=PF 3=ESTRANGEIRO
    nome_socio                    VARCHAR(255),
    cnpj_cpf_socio                VARCHAR(14),
    qualificacao_socio            CHAR(2),
    data_entrada_sociedade        CHAR(8),
    pais                          CHAR(3),
    cpf_representante_legal       CHAR(11),
    nome_representante_legal      VARCHAR(255),
    qualificacao_representante_legal CHAR(2),
    faixa_etaria                  CHAR(1),

    FOREIGN KEY (cnpj_basico) REFERENCES cnpj.empresas(cnpj_basico)
);

-- Índice para busca por CPF parcial ou completo
CREATE INDEX IF NOT EXISTS idx_socios_cpf
    ON cnpj.socios(cnpj_cpf_socio);

CREATE INDEX IF NOT EXISTS idx_socios_cnpj_basico
    ON cnpj.socios(cnpj_basico);

-- ─── Tabela: dados_simples ─────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS cnpj.dados_simples (
    cnpj_basico          CHAR(8)  PRIMARY KEY,
    opcao_simples        CHAR(1),
    data_opcao_simples   CHAR(8),
    data_exclusao_simples CHAR(8),
    opcao_mei            CHAR(1),
    data_opcao_mei       CHAR(8),
    data_exclusao_mei    CHAR(8),

    FOREIGN KEY (cnpj_basico) REFERENCES cnpj.empresas(cnpj_basico)
);
