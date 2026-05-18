package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dataws_api/internal/model"
)

// scanEmpresas lê linhas e carrega QSA + CNAEs secundários em lote (evita N+1).
func scanEmpresas(ctx context.Context, db *pgxpool.Pool, rows pgx.Rows, loadQSA bool) ([]*model.EmpresaResult, error) {
	var results []*model.EmpresaResult

	for rows.Next() {
		emp, err := scanEmpresaRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, emp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}
	if len(results) == 0 {
		return results, nil
	}

	if loadQSA {
		basicos := uniqueBasicos(results)
		qsaByBasico, err := loadSociosBatch(ctx, db, basicos)
		if err != nil {
			return nil, err
		}
		for _, emp := range results {
			emp.QSA = qsaByBasico[emp.CNPJBasico]
			if emp.QSA == nil {
				emp.QSA = []model.Socio{}
			}
		}
	} else {
		for _, emp := range results {
			emp.QSA = []model.Socio{}
		}
	}

	allCodes := collectCNAECodes(results)
	cnaeDesc, err := loadCNAEDescriptionsBatch(ctx, db, allCodes)
	if err != nil {
		return nil, err
	}
	for _, emp := range results {
		applyStaticLabels(emp)
		emp.CNAESecundariosDetalhados = buildCNAEItems(parseCNAECodes(emp.CNAESecundarios), cnaeDesc)
	}

	return results, nil
}

func scanEmpresaRow(rows pgx.Rows) (*model.EmpresaResult, error) {
	var emp model.EmpresaResult
	err := rows.Scan(
		&emp.CNPJBasico,
		&emp.RazaoSocial,
		&emp.NaturezaJuridicaCodigo,
		&emp.NaturezaJuridicaDescr,
		&emp.QualificacaoResponsavel,
		&emp.QualificacaoResponsavelDescr,
		&emp.CapitalSocial,
		&emp.Porte,
		&emp.EnteFederativoRespons,
		&emp.CNPJOrdem,
		&emp.CNPJDV,
		&emp.IdentificadorMatrizFil,
		&emp.NomeFantasia,
		&emp.SituacaoCadastral,
		&emp.DataSituacaoCadastral,
		&emp.MotivoSituacaoCodigo,
		&emp.MotivoSituacaoDescr,
		&emp.NomeCidadeExterior,
		&emp.PaisCodigo,
		&emp.PaisDescr,
		&emp.DataInicioAtividade,
		&emp.CNAEPrincipalCodigo,
		&emp.CNAEPrincipalDescr,
		&emp.CNAESecundarios,
		&emp.TipoLogradouro,
		&emp.Logradouro,
		&emp.Numero,
		&emp.Complemento,
		&emp.Bairro,
		&emp.CEP,
		&emp.UF,
		&emp.MunicipioCodigo,
		&emp.MunicipioDescr,
		&emp.DDD1,
		&emp.Telefone1,
		&emp.DDD2,
		&emp.Telefone2,
		&emp.DDDFax,
		&emp.Fax,
		&emp.Email,
		&emp.SituacaoEspecial,
		&emp.DataSituacaoEspecial,
		&emp.SimplesOpcao,
		&emp.SimplesDataOpcao,
		&emp.SimplesDataExclusao,
		&emp.MEIOpcao,
		&emp.MEIDataOpcao,
		&emp.MEIDataExclusao,
	)
	if err != nil {
		return nil, fmt.Errorf("scan empresa: %w", err)
	}
	emp.CNPJCompleto = emp.CNPJBasico + emp.CNPJOrdem + emp.CNPJDV
	return &emp, nil
}

func uniqueBasicos(results []*model.EmpresaResult) []string {
	seen := make(map[string]struct{}, len(results))
	out := make([]string, 0, len(results))
	for _, emp := range results {
		if _, ok := seen[emp.CNPJBasico]; ok {
			continue
		}
		seen[emp.CNPJBasico] = struct{}{}
		out = append(out, emp.CNPJBasico)
	}
	return out
}

func collectCNAECodes(results []*model.EmpresaResult) []string {
	seen := make(map[string]struct{})
	var codes []string
	for _, emp := range results {
		for _, c := range parseCNAECodes(emp.CNAESecundarios) {
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			codes = append(codes, c)
		}
	}
	return codes
}

func loadSociosBatch(ctx context.Context, db *pgxpool.Pool, basicos []string) (map[string][]model.Socio, error) {
	out := make(map[string][]model.Socio, len(basicos))
	if len(basicos) == 0 {
		return out, nil
	}

	rows, err := db.Query(ctx, `
		SELECT
			s.cnpj_basico,
			COALESCE(s.identificador_de_socio, ''),
			COALESCE(s.nome_socio, ''),
			COALESCE(s.cnpj_cpf_do_socio, ''),
			COALESCE(s.qualificacao_do_socio, ''),
			COALESCE(qs.descricao, ''),
			COALESCE(s.data_entrada_sociedade::text, ''),
			COALESCE(s.pais, ''),
			COALESCE(p.descricao, ''),
			COALESCE(s.representante_legal, ''),
			COALESCE(s.nome_do_representante, ''),
			COALESCE(s.qualificacao_do_representante_legal, ''),
			COALESCE(qr.descricao, ''),
			COALESCE(s.faixa_etaria, '')
		FROM cnpj.socios s
		LEFT JOIN cnpj.qualificacoes_socios qs ON qs.codigo = s.qualificacao_do_socio
		LEFT JOIN cnpj.paises p                ON p.codigo  = s.pais
		LEFT JOIN cnpj.qualificacoes_socios qr ON qr.codigo = s.qualificacao_do_representante_legal
		WHERE s.cnpj_basico = ANY($1)
		ORDER BY s.cnpj_basico, s.nome_socio`, basicos)
	if err != nil {
		return nil, fmt.Errorf("loadSociosBatch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var basico string
		var s model.Socio
		if err := rows.Scan(
			&basico,
			&s.IdentificadorSocio,
			&s.NomeSocio,
			&s.CNPJCPFSocio,
			&s.QualificacaoCodigo,
			&s.QualificacaoDescr,
			&s.DataEntradaSociedade,
			&s.PaisCodigo,
			&s.PaisDescr,
			&s.CPFRepresentanteLegal,
			&s.NomeRepresentanteLegal,
			&s.QualifRepresentanteCod,
			&s.QualifRepresentanteDescr,
			&s.FaixaEtaria,
		); err != nil {
			return nil, err
		}
		s.IdentificadorSocioDescr = model.IdentificadorSocioDescricao(s.IdentificadorSocio)
		s.FaixaEtariaDescr = model.FaixaEtariaDescricao(s.FaixaEtaria)
		out[basico] = append(out[basico], s)
	}
	return out, rows.Err()
}

func loadCNAEDescriptionsBatch(ctx context.Context, db *pgxpool.Pool, codes []string) (map[string]string, error) {
	out := make(map[string]string)
	if len(codes) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `
		SELECT codigo, COALESCE(descricao, '')
		FROM cnpj.cnaes
		WHERE codigo = ANY($1)`, codes)
	if err != nil {
		return nil, fmt.Errorf("loadCNAEDescriptionsBatch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var codigo, desc string
		if err := rows.Scan(&codigo, &desc); err != nil {
			return nil, err
		}
		out[strings.TrimSpace(codigo)] = desc
	}
	return out, rows.Err()
}

func applyStaticLabels(emp *model.EmpresaResult) {
	emp.PorteDescr = model.PorteDescricao(emp.Porte)
	emp.SituacaoCadastralDescr = model.SituacaoCadastralDescricao(emp.SituacaoCadastral)
	emp.IdentificadorMatrizFilDescr = model.IdentificadorMatrizFilialDescricao(emp.IdentificadorMatrizFil)
}

func buildCNAEItems(codes []string, desc map[string]string) []model.CNAEItem {
	if len(codes) == 0 {
		return []model.CNAEItem{}
	}
	out := make([]model.CNAEItem, 0, len(codes))
	for _, c := range codes {
		out = append(out, model.CNAEItem{Codigo: c, Descricao: desc[c]})
	}
	return out
}
