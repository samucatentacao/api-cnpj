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

	results, err := scanEmpresas(ctx, r.db, rows, true)
	if err != nil {
		return nil, fmt.Errorf("postgres.GetByCNPJ: %w", err)
	}
	if len(results) == 0 {
		return nil, model.ErrNotFound
	}
	return results[0], nil
}

// ─── GetRandom ───────────────────────────────────────────────────────────────

// GetRandom escolhe um estabelecimento ao acerto usando TABLESAMPLE (rápido em tabelas grandes)
// e depois carrega o registro completo via GetByCNPJ.
func (r *empresaRepository) GetRandom(ctx context.Context) (*model.EmpresaResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	innerLimit := 8000
	// SYSTEM(n) ≈ n% dos blocos físicos; valores crescentes se a amostra vier vazia.
	for _, pct := range []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0} {
		query := fmt.Sprintf(`
			SELECT cnpj_basico, cnpj_ordem, cnpj_dv
			FROM (
				SELECT cnpj_basico, cnpj_ordem, cnpj_dv
				FROM cnpj.estabelecimentos TABLESAMPLE SYSTEM(%g)
				LIMIT %d
			) s
			ORDER BY random()
			LIMIT 1`, pct, innerLimit)

		var basico, ordem, dv string
		err := r.db.QueryRow(ctx, query).Scan(&basico, &ordem, &dv)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("postgres.GetRandom: %w", err)
		}
		if basico == "" || ordem == "" || dv == "" {
			continue
		}
		cnpj := basico + ordem + dv
		return r.GetByCNPJ(ctx, cnpj)
	}

	return nil, model.ErrRandomSample
}

// ─── Search ──────────────────────────────────────────────────────────────────

func (r *empresaRepository) Search(ctx context.Context, f model.SearchFilter) ([]*model.EmpresaResult, int, error) {
	// Busca via cnpj.socios (CPF mascarado e/ou nome do sócio), sem nome/CNPJ da empresa
	if f.Nome == "" && f.CNPJ == "" && r.canSearchViaSocios(f) {
		return r.searchViaSocios(ctx, f)
	}

	timeout := 30 * time.Second
	if f.CPF != "" {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
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

	// ── CPF / nome do sócio (caminho lento — ILIKE / parcial curto) ─────────────
	if f.CPF != "" {
		cpf := sanitize(f.CPF)
		cond, condArgs := cpfSocioFilter(n, cpf)
		conditions = append(conditions, cond)
		args = append(args, condArgs...)
		n += len(condArgs)
	}
	if f.NomeSocio != "" {
		conditions = append(conditions, fmt.Sprintf(`e.cnpj_basico IN (
			SELECT DISTINCT s.cnpj_basico FROM cnpj.socios s
			WHERE s.nome_socio ILIKE '%%' || $%d || '%%'
		)`, n))
		args = append(args, f.NomeSocio)
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

	results, err := scanEmpresas(ctx, r.db, rows, true)
	if err != nil {
		return nil, 0, err
	}

	return results, total, nil
}

func (r *empresaRepository) canSearchViaSocios(f model.SearchFilter) bool {
	if f.NomeSocio != "" {
		return true
	}
	cpf := sanitize(f.CPF)
	return len(cpf) == 6 || len(cpf) == 11
}

// searchViaSocios busca empresas a partir de cnpj.socios (CPF mascarado e/ou nome_socio).
func (r *empresaRepository) searchViaSocios(ctx context.Context, f model.SearchFilter) ([]*model.EmpresaResult, int, error) {
	if f.CPF != "" && f.NomeSocio != "" {
		cpf := sanitize(f.CPF)
		if len(cpf) == 6 || len(cpf) == 11 {
			return r.searchViaSociosCPFAndNome(ctx, f, cpf)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	socioSQL, socioArgs, err := buildSocioWhereClause(f, 1)
	if err != nil {
		return nil, 0, err
	}

	var total int
	if err := r.db.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(DISTINCT s.cnpj_basico) FROM cnpj.socios s WHERE %s`, socioSQL),
		socioArgs...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres.searchViaSocios count: %w", err)
	}

	keyArgs := make([]any, 0, 12)
	keyArgs = append(keyArgs, socioArgs...)
	keyN := len(keyArgs) + 1
	keyWhere := []string{fmt.Sprintf(`est.cnpj_basico IN (
		SELECT DISTINCT s.cnpj_basico FROM cnpj.socios s WHERE %s
	)`, socioSQL)}

	if f.UF != "" {
		keyWhere = append(keyWhere, fmt.Sprintf("est.uf = $%d", keyN))
		keyArgs = append(keyArgs, strings.ToUpper(f.UF))
		keyN++
	}
	if f.SituacaoCadastral != "" {
		keyWhere = append(keyWhere, fmt.Sprintf("est.situacao_cadastral = $%d", keyN))
		keyArgs = append(keyArgs, f.SituacaoCadastral)
		keyN++
	}
	if f.CNAE != "" {
		keyWhere = append(keyWhere, fmt.Sprintf("est.cnae_fiscal_principal = $%d", keyN))
		keyArgs = append(keyArgs, f.CNAE)
		keyN++
	}
	if f.Municipio != "" {
		keyWhere = append(keyWhere, fmt.Sprintf(`est.municipio IN (
			SELECT codigo FROM cnpj.municipios WHERE descricao ILIKE '%%' || $%d || '%%'
		)`, keyN))
		keyArgs = append(keyArgs, f.Municipio)
		keyN++
	}
	if f.Porte != "" {
		keyWhere = append(keyWhere, fmt.Sprintf(`est.cnpj_basico IN (
			SELECT cnpj_basico FROM cnpj.empresas WHERE porte = $%d
		)`, keyN))
		keyArgs = append(keyArgs, f.Porte)
		keyN++
	}

	keyQ := fmt.Sprintf(`
		SELECT est.cnpj_basico, est.cnpj_ordem, est.cnpj_dv
		FROM cnpj.estabelecimentos est
		WHERE %s
		ORDER BY est.cnpj_basico, est.cnpj_ordem
		LIMIT $%d OFFSET $%d`,
		strings.Join(keyWhere, " AND "), keyN, keyN+1)
	keyArgs = append(keyArgs, limit, offset)

	keyRows, err := r.db.Query(ctx, keyQ, keyArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres.searchViaSocios keys: %w", err)
	}
	defer keyRows.Close()

	type estKey struct{ basico, ordem, dv string }
	var keys []estKey
	for keyRows.Next() {
		var k estKey
		if err := keyRows.Scan(&k.basico, &k.ordem, &k.dv); err != nil {
			return nil, 0, err
		}
		keys = append(keys, k)
	}
	if err := keyRows.Err(); err != nil {
		return nil, 0, err
	}
	if len(keys) == 0 {
		return []*model.EmpresaResult{}, total, nil
	}

	tuples := make([]string, len(keys))
	detailArgs := make([]any, 0, len(keys)*3)
	for i, k := range keys {
		p := len(detailArgs) + 1
		tuples[i] = fmt.Sprintf("($%d,$%d,$%d)", p, p+1, p+2)
		detailArgs = append(detailArgs, k.basico, k.ordem, k.dv)
	}

	detailQ := baseSelectQuery() + fmt.Sprintf(`
		WHERE (e.cnpj_basico, est.cnpj_ordem, est.cnpj_dv) IN (%s)
		ORDER BY e.razao_social`, strings.Join(tuples, ","))

	rows, err := r.db.Query(ctx, detailQ, detailArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres.searchViaSocios detail: %w", err)
	}
	defer rows.Close()

	results, err := scanEmpresas(ctx, r.db, rows, true)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// searchViaSociosCPFAndNome: filtra primeiro por CPF (rápido), depois nome do sócio em memória.
func (r *empresaRepository) searchViaSociosCPFAndNome(ctx context.Context, f model.SearchFilter, cpf string) ([]*model.EmpresaResult, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	masked, digits := cpfMasked(cpf)
	nomeNeedle := strings.ToUpper(f.NomeSocio)

	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT s.cnpj_basico,
		       COALESCE(s.nome_socio, ''),
		       COALESCE(s.nome_do_representante, ''),
		       COALESCE(s.cnpj_cpf_do_socio, ''),
		       COALESCE(s.representante_legal, '')
		FROM cnpj.socios s
		WHERE s.cnpj_cpf_do_socio = $1
		   OR s.representante_legal = $1
		   OR s.representante_legal LIKE '%' || $2 || '%'`, masked, digits)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres.searchViaSociosCPFAndNome socios: %w", err)
	}
	defer rows.Close()

	basicoSet := make(map[string]struct{})
	for rows.Next() {
		var basico, nomeSocio, nomeRep, doc, rep string
		if err := rows.Scan(&basico, &nomeSocio, &nomeRep, &doc, &rep); err != nil {
			return nil, 0, err
		}
		if strings.Contains(strings.ToUpper(nomeSocio), nomeNeedle) ||
			strings.Contains(strings.ToUpper(nomeRep), nomeNeedle) {
			basicoSet[basico] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	basicos := make([]string, 0, len(basicoSet))
	for b := range basicoSet {
		basicos = append(basicos, b)
	}
	total := len(basicos)
	if total == 0 {
		return []*model.EmpresaResult{}, 0, nil
	}

	return r.fetchEstabelecimentosForBasicos(ctx, f, basicos, total)
}

func cpfMasked(cpf string) (masked, digits string) {
	digits = cpf
	if len(cpf) == 11 {
		digits = cpf[3:9]
	}
	return "***" + digits + "**", digits
}

// fetchEstabelecimentosForBasicos lista estabelecimentos dos cnpj_basico e carrega detalhe.
func (r *empresaRepository) fetchEstabelecimentosForBasicos(ctx context.Context, f model.SearchFilter, basicos []string, total int) ([]*model.EmpresaResult, int, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	placeholders := make([]string, len(basicos))
	args := make([]any, len(basicos))
	for i, b := range basicos {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = b
	}
	n := len(basicos) + 1

	keyWhere := []string{fmt.Sprintf("est.cnpj_basico IN (%s)", strings.Join(placeholders, ","))}
	if f.UF != "" {
		keyWhere = append(keyWhere, fmt.Sprintf("est.uf = $%d", n))
		args = append(args, strings.ToUpper(f.UF))
		n++
	}
	if f.SituacaoCadastral != "" {
		keyWhere = append(keyWhere, fmt.Sprintf("est.situacao_cadastral = $%d", n))
		args = append(args, f.SituacaoCadastral)
		n++
	}
	if f.CNAE != "" {
		keyWhere = append(keyWhere, fmt.Sprintf("est.cnae_fiscal_principal = $%d", n))
		args = append(args, f.CNAE)
		n++
	}
	if f.Municipio != "" {
		keyWhere = append(keyWhere, fmt.Sprintf(`est.municipio IN (
			SELECT codigo FROM cnpj.municipios WHERE descricao ILIKE '%%' || $%d || '%%'
		)`, n))
		args = append(args, f.Municipio)
		n++
	}
	if f.Porte != "" {
		keyWhere = append(keyWhere, fmt.Sprintf(`est.cnpj_basico IN (
			SELECT cnpj_basico FROM cnpj.empresas WHERE porte = $%d
		)`, n))
		args = append(args, f.Porte)
		n++
	}

	keyQ := fmt.Sprintf(`
		SELECT est.cnpj_basico, est.cnpj_ordem, est.cnpj_dv
		FROM cnpj.estabelecimentos est
		WHERE %s
		ORDER BY est.cnpj_basico, est.cnpj_ordem
		LIMIT $%d OFFSET $%d`,
		strings.Join(keyWhere, " AND "), n, n+1)
	args = append(args, limit, offset)

	keyRows, err := r.db.Query(ctx, keyQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("fetchEstabelecimentosForBasicos keys: %w", err)
	}
	defer keyRows.Close()

	type estKey struct{ basico, ordem, dv string }
	var keys []estKey
	for keyRows.Next() {
		var k estKey
		if err := keyRows.Scan(&k.basico, &k.ordem, &k.dv); err != nil {
			return nil, 0, err
		}
		keys = append(keys, k)
	}
	if err := keyRows.Err(); err != nil {
		return nil, 0, err
	}
	if len(keys) == 0 {
		return []*model.EmpresaResult{}, total, nil
	}

	tuples := make([]string, len(keys))
	detailArgs := make([]any, 0, len(keys)*3)
	for i, k := range keys {
		p := len(detailArgs) + 1
		tuples[i] = fmt.Sprintf("($%d,$%d,$%d)", p, p+1, p+2)
		detailArgs = append(detailArgs, k.basico, k.ordem, k.dv)
	}

	detailQ := baseSelectQuery() + fmt.Sprintf(`
		WHERE (e.cnpj_basico, est.cnpj_ordem, est.cnpj_dv) IN (%s)
		ORDER BY e.razao_social`, strings.Join(tuples, ","))

	drows, err := r.db.Query(ctx, detailQ, detailArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("fetchEstabelecimentosForBasicos detail: %w", err)
	}
	defer drows.Close()

	results, err := scanEmpresas(ctx, r.db, drows, true)
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

func scanEmpresas(ctx context.Context, db *pgxpool.Pool, rows pgx.Rows, loadQSA bool) ([]*model.EmpresaResult, error) {
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

		if loadQSA {
			socios, err := loadSocios(ctx, db, emp.CNPJBasico)
			if err != nil {
				return nil, err
			}
			emp.QSA = socios
		} else {
			emp.QSA = []model.Socio{}
		}
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

// buildSocioWhereClause monta WHERE em cnpj.socios para CPF (mascarado) e/ou nome_socio.
func buildSocioWhereClause(f model.SearchFilter, startN int) (string, []any, error) {
	parts := []string{}
	args := []any{}
	n := startN

	if f.CPF != "" {
		cpf := sanitize(f.CPF)
		if len(cpf) != 6 && len(cpf) != 11 {
			return "", nil, fmt.Errorf("CPF deve ter 6 ou 11 dígitos no caminho rápido")
		}
		digits := cpf
		if len(cpf) == 11 {
			digits = cpf[3:9]
		}
		masked := "***" + digits + "**"
		parts = append(parts, fmt.Sprintf(`(
			s.cnpj_cpf_do_socio = $%d
			OR s.representante_legal = $%d
			OR s.representante_legal LIKE '%%' || $%d || '%%'
		)`, n, n, n+1))
		args = append(args, masked, digits)
		n += 2
	}

	if f.NomeSocio != "" {
		parts = append(parts, fmt.Sprintf(`s.nome_socio ILIKE '%%' || $%d || '%%'`, n))
		args = append(args, f.NomeSocio)
		n++
	}

	if len(parts) == 0 {
		return "", nil, fmt.Errorf("nenhum critério de sócio informado")
	}

	return strings.Join(parts, " AND "), args, nil
}

// cpfSocioFilter monta condição IN (socios) para CPF parcial (4–5 dígitos) com ILIKE.
func cpfSocioFilter(startN int, cpf string) (string, []any) {
	match := func(n int) string {
		return fmt.Sprintf(`(
			s.cnpj_cpf_do_socio ILIKE '%%' || $%d || '%%'
			OR s.representante_legal ILIKE '%%' || $%d || '%%'
		)`, n, n)
	}
	n := startN
	cond := fmt.Sprintf(`e.cnpj_basico IN (
		SELECT DISTINCT s.cnpj_basico FROM cnpj.socios s
		WHERE %s
	)`, match(n))
	return cond, []any{cpf}
}

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
