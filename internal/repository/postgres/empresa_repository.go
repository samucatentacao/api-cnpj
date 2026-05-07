package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dataws_api/internal/model"
)

type empresaRepository struct {
	db *pgxpool.Pool
}

func NewEmpresaRepository(db *pgxpool.Pool) model.EmpresaRepository {
	return &empresaRepository{db: db}
}

// ─── GetByCNPJ ───────────────────────────────────────────────────────────────

func (r *empresaRepository) GetByCNPJ(ctx context.Context, cnpj string) (*model.EmpresaResult, error) {
	basico := cnpj[:8]
	ordem := cnpj[8:12]
	dv := cnpj[12:14]

	query := baseSelectQuery() + `
		WHERE e.cnpj_basico = $1
		  AND est.cnpj_ordem = $2
		  AND est.cnpj_dv    = $3
		LIMIT 1`

	rows, err := r.db.Query(ctx, query, basico, ordem, dv)
	if err != nil {
		return nil, fmt.Errorf("postgres.GetByCNPJ: %w", err)
	}
	defer rows.Close()

	results, err := scanEmpresas(ctx, r.db, rows)
	if err != nil {
		return nil, fmt.Errorf("postgres.GetByCNPJ: %w", err)
	}
	if len(results) == 0 {
		return nil, model.ErrNotFound
	}
	return results[0], nil
}

// ─── Search ──────────────────────────────────────────────────────────────────

func (r *empresaRepository) Search(ctx context.Context, f model.SearchFilter) ([]*model.EmpresaResult, int, error) {
	// Timeout de 30s para evitar queries longas em tabelas grandes sem índice
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []any{}
	conditions := []string{}
	n := 1

	// ── CNPJ ─────────────────────────────────────────────────────────────────
	if f.CNPJ != "" {
		cnpj := sanitize(f.CNPJ)
		switch len(cnpj) {
		case 14:
			conditions = append(conditions,
				fmt.Sprintf("(e.cnpj_basico || est.cnpj_ordem || est.cnpj_dv) = $%d", n))
			args = append(args, cnpj)
		case 8:
			conditions = append(conditions, fmt.Sprintf("e.cnpj_basico = $%d", n))
			args = append(args, cnpj)
		default:
			conditions = append(conditions, fmt.Sprintf("e.cnpj_basico LIKE $%d", n))
			args = append(args, cnpj+"%")
		}
		n++
	}

	// ── Nome (razão social ou nome fantasia) ──────────────────────────────────
	if f.Nome != "" {
		conditions = append(conditions, fmt.Sprintf(`(
			e.razao_social ILIKE '%%' || $%d || '%%'
			OR est.nome_fantasia ILIKE '%%' || $%d || '%%'
		)`, n, n))
		args = append(args, f.Nome)
		n++
	}

	// ── CPF de sócio (completo ou parcial) ────────────────────────────────────
	if f.CPF != "" {
		cpf := sanitize(f.CPF)
		if len(cpf) == 11 {
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1 FROM cnpj.socios s2
				WHERE s2.cnpj_basico = e.cnpj_basico
				  AND s2.cnpj_cpf_do_socio = $%d
			)`, n))
		} else {
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1 FROM cnpj.socios s2
				WHERE s2.cnpj_basico = e.cnpj_basico
				  AND s2.cnpj_cpf_do_socio LIKE '%%' || $%d || '%%'
			)`, n))
		}
		args = append(args, cpf)
		n++
	}

	// ── Filtros adicionais ────────────────────────────────────────────────────
	if f.UF != "" {
		conditions = append(conditions, fmt.Sprintf("est.uf = $%d", n))
		args = append(args, strings.ToUpper(f.UF))
		n++
	}
	if f.Municipio != "" {
		conditions = append(conditions, fmt.Sprintf("mun.descricao ILIKE '%%' || $%d || '%%'", n))
		args = append(args, f.Municipio)
		n++
	}
	if f.SituacaoCadastral != "" {
		conditions = append(conditions, fmt.Sprintf("est.situacao_cadastral = $%d", n))
		args = append(args, f.SituacaoCadastral)
		n++
	}
	if f.CNAE != "" {
		conditions = append(conditions, fmt.Sprintf("est.cnae_fiscal_principal = $%d", n))
		args = append(args, f.CNAE)
		n++
	}
	if f.Porte != "" {
		conditions = append(conditions, fmt.Sprintf("e.porte = $%d", n))
		args = append(args, f.Porte)
		n++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, "\n  AND ")
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	// COUNT total
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM cnpj.empresas e
		JOIN cnpj.estabelecimentos est ON est.cnpj_basico = e.cnpj_basico
		LEFT JOIN cnpj.municipios mun ON mun.codigo = est.municipio
		%s`, where)

	var total int
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres.Search count: %w", err)
	}

	// Dados paginados
	dataQuery := baseSelectQuery() + "\n" + where +
		fmt.Sprintf("\nORDER BY e.razao_social\nLIMIT $%d OFFSET $%d", n, n+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres.Search query: %w", err)
	}
	defer rows.Close()

	results, err := scanEmpresas(ctx, r.db, rows)
	if err != nil {
		return nil, 0, err
	}

	return results, total, nil
}

// ─── Query base com todos os JOINs usando nomes reais das colunas ─────────────

func baseSelectQuery() string {
	return `
	SELECT
		e.cnpj_basico,
		COALESCE(e.razao_social, ''),
		COALESCE(e.natureza_juridica, ''),
		COALESCE(nj.descricao, ''),
		COALESCE(e.qualificacao_responsavel, ''),
		COALESCE(e.capital_social, 0),
		COALESCE(e.porte, ''),
		COALESCE(e.ente_federativo_responsavel, ''),

		est.cnpj_ordem,
		est.cnpj_dv,
		COALESCE(est.identificador_matriz_filial::text, ''),
		COALESCE(est.nome_fantasia, ''),
		COALESCE(est.situacao_cadastral, ''),
		COALESCE(est.data_situacao_cadastral::text, ''),
		COALESCE(est.motivo_situacao_cadastral, ''),
		COALESCE(mot.descricao, ''),
		COALESCE(est.nome_cidade_exterior, ''),
		COALESCE(est.pais, ''),
		COALESCE(pai.descricao, ''),
		COALESCE(est.data_inicio_atividade::text, ''),
		COALESCE(est.cnae_fiscal_principal, ''),
		COALESCE(cn.descricao, ''),
		COALESCE(est.cnae_fiscal_secundaria, ''),
		COALESCE(est.tipo_logradouro, ''),
		COALESCE(est.logradouro, ''),
		COALESCE(est.numero, ''),
		COALESCE(est.complemento, ''),
		COALESCE(est.bairro, ''),
		COALESCE(est.cep, ''),
		COALESCE(est.uf, ''),
		COALESCE(est.municipio, ''),
		COALESCE(mun.descricao, ''),
		COALESCE(est.ddd_1, ''),
		COALESCE(est.telefone_1, ''),
		COALESCE(est.ddd_2, ''),
		COALESCE(est.telefone_2, ''),
		COALESCE(est.ddd_fax, ''),
		COALESCE(est.fax, ''),
		COALESCE(est.correio_eletronico, ''),
		COALESCE(est.situacao_especial, ''),
		COALESCE(est.data_situacao_especial::text, ''),

		COALESCE(ds.opcao_pelo_simples, ''),
		COALESCE(ds.data_opcao_pelo_simples::text, ''),
		COALESCE(ds.data_exclusao_do_simples::text, ''),
		COALESCE(ds.opcao_pelo_mei, ''),
		COALESCE(ds.data_opcao_pelo_mei::text, ''),
		COALESCE(ds.data_exclusao_do_mei::text, '')

	FROM cnpj.empresas e
	JOIN cnpj.estabelecimentos est          ON est.cnpj_basico = e.cnpj_basico
	LEFT JOIN cnpj.naturezas_juridicas nj   ON nj.codigo = e.natureza_juridica
	LEFT JOIN cnpj.qualificacoes_socios qs  ON qs.codigo = e.qualificacao_responsavel
	LEFT JOIN cnpj.municipios mun           ON mun.codigo = est.municipio
	LEFT JOIN cnpj.motivos mot              ON mot.codigo = est.motivo_situacao_cadastral
	LEFT JOIN cnpj.cnaes cn                ON cn.codigo = est.cnae_fiscal_principal
	LEFT JOIN cnpj.paises pai              ON pai.codigo = est.pais
	LEFT JOIN cnpj.dados_simples ds        ON ds.cnpj_basico = e.cnpj_basico`
}

// ─── Scanner ─────────────────────────────────────────────────────────────────

func scanEmpresas(ctx context.Context, db *pgxpool.Pool, rows pgx.Rows) ([]*model.EmpresaResult, error) {
	var results []*model.EmpresaResult

	for rows.Next() {
		var emp model.EmpresaResult
		err := rows.Scan(
			&emp.CNPJBasico,
			&emp.RazaoSocial,
			&emp.NaturezaJuridicaCodigo,
			&emp.NaturezaJuridicaDescr,
			&emp.QualificacaoResponsavel,
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

		socios, err := loadSocios(ctx, db, emp.CNPJBasico)
		if err != nil {
			return nil, err
		}
		emp.QSA = socios
		results = append(results, &emp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}
	return results, nil
}

// ─── Sócios ──────────────────────────────────────────────────────────────────

func loadSocios(ctx context.Context, db *pgxpool.Pool, cnpjBasico string) ([]model.Socio, error) {
	query := `
		SELECT
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
		WHERE s.cnpj_basico = $1
		ORDER BY s.nome_socio`

	rows, err := db.Query(ctx, query, cnpjBasico)
	if err != nil {
		return nil, fmt.Errorf("loadSocios: %w", err)
	}
	defer rows.Close()

	var socios []model.Socio
	for rows.Next() {
		var s model.Socio
		if err := rows.Scan(
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
			return nil, fmt.Errorf("scan socio: %w", err)
		}
		socios = append(socios, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("socios rows err: %w", err)
	}
	return socios, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func sanitize(s string) string {
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "-", "")
	return strings.TrimSpace(s)
}

func errCheck(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	return err
}
