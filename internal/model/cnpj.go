package model

// CNAEItem é um CNAE com código e descrição (principal ou secundário).
type CNAEItem struct {
	Codigo     string `json:"codigo"`
	Descricao  string `json:"descricao"`
}

// EmpresaResult é a visão consolidada devolvida pela API,
// montada com JOIN entre empresas, estabelecimentos, sócios e tabelas auxiliares.
type EmpresaResult struct {
	// ── Empresa (cnpj.empresas) ───────────────────────────────────────────────
	CNPJBasico                   string  `json:"cnpj_basico"`
	RazaoSocial                  string  `json:"razao_social"`
	NaturezaJuridicaCodigo       string  `json:"natureza_juridica_codigo"`
	NaturezaJuridicaDescr        string  `json:"natureza_juridica"`
	QualificacaoResponsavel      string  `json:"qualificacao_responsavel"`
	QualificacaoResponsavelDescr string  `json:"qualificacao_responsavel_descricao"`
	CapitalSocial                float64 `json:"capital_social"`
	Porte                        string  `json:"porte"`
	PorteDescr                   string  `json:"porte_descricao"`
	EnteFederativoRespons        string  `json:"ente_federativo_responsavel"`

	// ── Estabelecimento ───────────────────────────────────────────────────────
	CNPJCompleto               string `json:"cnpj"`
	CNPJOrdem                  string `json:"cnpj_ordem"`
	CNPJDV                     string `json:"cnpj_dv"`
	IdentificadorMatrizFil     string `json:"identificador_matriz_filial"`
	IdentificadorMatrizFilDescr string `json:"identificador_matriz_filial_descricao"`
	NomeFantasia               string `json:"nome_fantasia"`
	SituacaoCadastral          string `json:"situacao_cadastral"`
	SituacaoCadastralDescr     string `json:"situacao_cadastral_descricao"`
	DataSituacaoCadastral      string `json:"data_situacao_cadastral"`
	MotivoSituacaoCodigo       string `json:"motivo_situacao_codigo"`
	MotivoSituacaoDescr        string `json:"motivo_situacao"`
	NomeCidadeExterior         string `json:"nome_cidade_exterior"`
	PaisCodigo                 string `json:"pais_codigo"`
	PaisDescr                  string `json:"pais"`
	DataInicioAtividade        string `json:"data_inicio_atividade"`
	CNAEPrincipalCodigo        string `json:"cnae_fiscal_principal"`
	CNAEPrincipalDescr         string `json:"cnae_fiscal_principal_descricao"`
	CNAESecundarios            string `json:"cnae_fiscal_secundario"`
	CNAESecundariosDetalhados  []CNAEItem `json:"cnaes_fiscais_secundarios"`
	TipoLogradouro             string `json:"tipo_logradouro"`
	Logradouro                 string `json:"logradouro"`
	Numero                     string `json:"numero"`
	Complemento                string `json:"complemento"`
	Bairro                     string `json:"bairro"`
	CEP                        string `json:"cep"`
	UF                         string `json:"uf"`
	MunicipioCodigo            string `json:"municipio_codigo"`
	MunicipioDescr             string `json:"municipio"`
	DDD1                       string `json:"ddd1"`
	Telefone1                  string `json:"telefone1"`
	DDD2                       string `json:"ddd2"`
	Telefone2                  string `json:"telefone2"`
	DDDFax                     string `json:"ddd_fax"`
	Fax                        string `json:"fax"`
	Email                      string `json:"email"`
	SituacaoEspecial           string `json:"situacao_especial"`
	DataSituacaoEspecial       string `json:"data_situacao_especial"`

	// ── Simples Nacional (cnpj.dados_simples) ─────────────────────────────────
	SimplesOpcao        string `json:"simples_opcao"`
	SimplesDataOpcao    string `json:"simples_data_opcao"`
	SimplesDataExclusao string `json:"simples_data_exclusao"`
	MEIOpcao            string `json:"mei_opcao"`
	MEIDataOpcao        string `json:"mei_data_opcao"`
	MEIDataExclusao     string `json:"mei_data_exclusao"`

	// ── Quadro Societário (cnpj.socios) — sempre completo ─────────────────────
	QSA []Socio `json:"qsa"`
}

// Socio representa um sócio/administrador da empresa.
type Socio struct {
	IdentificadorSocio         string `json:"identificador_socio"`
	IdentificadorSocioDescr    string `json:"identificador_socio_descricao"`
	NomeSocio                  string `json:"nome_socio"`
	CNPJCPFSocio               string `json:"cnpj_cpf_socio"`
	QualificacaoCodigo         string `json:"qualificacao_codigo"`
	QualificacaoDescr          string `json:"qualificacao_socio"`
	DataEntradaSociedade       string `json:"data_entrada_sociedade"`
	PaisCodigo                 string `json:"pais_codigo"`
	PaisDescr                  string `json:"pais"`
	CPFRepresentanteLegal      string `json:"cpf_representante_legal"`
	NomeRepresentanteLegal     string `json:"nome_representante_legal"`
	QualifRepresentanteCod     string `json:"qualificacao_representante_codigo"`
	QualifRepresentanteDescr   string `json:"qualificacao_representante"`
	FaixaEtaria                string `json:"faixa_etaria"`
	FaixaEtariaDescr           string `json:"faixa_etaria_descricao"`
}

// SearchFilter encapsula todos os parâmetros de busca possíveis.
type SearchFilter struct {
	CNPJ              string `form:"cnpj"`
	Nome              string `form:"nome"`
	CPF               string `form:"cpf"`
	NomeSocio         string `form:"nome_socio"`
	UF                string `form:"uf"`
	Municipio         string `form:"municipio"`
	SituacaoCadastral string `form:"situacao_cadastral"`
	CNAE              string `form:"cnae"`
	Porte             string `form:"porte"`
	Page              int    `form:"page"`
	Limit             int    `form:"limit"`
}
