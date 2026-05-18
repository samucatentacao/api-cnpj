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
	// CNPJ completo (14 dígitos) sozinho → mesma rota rápida de GET /cnpjs/:cnpj (PK).
	if isExactCNPJOnlySearch(f) {
		return r.searchByCNPJExact(ctx, f)
	}

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
				fmt.Sprintf("e.cnpj_basico = $%d AND est.cnpj_ordem = $%d AND est.cnpj_dv = $%d", n, n+1, n+2))
			args = append(args, cnpj[:8], cnpj[8:12], cnpj[12:14])
			n += 3
		case 8:
			conditions = append(conditions, fmt.Sprintf("e.cnpj_basico = $%d", n))
			args = append(args, cnpj)
			n++
		default:
			conditions = append(conditions, fmt.Sprintf("e.cnpj_basico LIKE $%d", n))
			args = append(args, cnpj+"%")
			n++
		}
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

	var total int
	// COUNT desnecessário quando o filtro é CNPJ de 14 dígitos (no máximo 1 estabelecimento).
	if len(conditions) == 1 && f.CNPJ != "" && len(sanitize(f.CNPJ)) == 14 {
		total = 1
	} else {
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM cnpj.empresas e
			JOIN cnpj.estabelecimentos est ON est.cnpj_basico = e.cnpj_basico
			LEFT JOIN cnpj.municipios mun ON mun.codigo = est.municipio
			%s`, where)
		if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("postgres.Search count: %w", err)
		}
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

func isExactCNPJOnlySearch(f model.SearchFilter) bool {
	if f.CNPJ == "" || f.Nome != "" || f.CPF != "" || f.NomeSocio != "" {
		return false
	}
	if f.UF != "" || f.Municipio != "" || f.SituacaoCadastral != "" || f.CNAE != "" || f.Porte != "" {
		return false
	}
	return len(sanitize(f.CNPJ)) == 14
}

func (r *empresaRepository) searchByCNPJExact(ctx context.Context, f model.SearchFilter) ([]*model.EmpresaResult, int, error) {
	cnpj := sanitize(f.CNPJ)
	result, err := r.GetByCNPJ(ctx, cnpj)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return []*model.EmpresaResult{}, 0, nil
		}
		return nil, 0, err
	}
	return []*model.EmpresaResult{result}, 1, nil
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
	cpf := sanitize(f.CPF)
	if f.CPF != "" && (len(cpf) == 6 || len(cpf) == 11) {
		if f.NomeSocio != "" {
			return r.searchViaSociosCPFAndNome(ctx, f, cpf)
		}
		return r.searchViaSociosCPF(ctx, f, cpf)
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

	args := make([]any, 0, 16)
	args = append(args, socioArgs...)
	n := len(args) + 1

	conditions := []string{fmt.Sprintf(`e.cnpj_basico IN (
		SELECT DISTINCT s.cnpj_basico FROM cnpj.socios s WHERE %s
	)`, socioSQL)}

	if f.UF != "" {
		conditions = append(conditions, fmt.Sprintf("est.uf = $%d", n))
		args = append(args, strings.ToUpper(f.UF))
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
	if f.Municipio != "" {
		conditions = append(conditions, fmt.Sprintf(`est.municipio IN (
			SELECT codigo FROM cnpj.municipios WHERE descricao ILIKE '%%' || $%d || '%%'
		)`, n))
		args = append(args, f.Municipio)
		n++
	}
	if f.Porte != "" {
		conditions = append(conditions, fmt.Sprintf("e.porte = $%d", n))
		args = append(args, f.Porte)
		n++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int
	countQ := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM cnpj.empresas e
		JOIN cnpj.estabelecimentos est ON est.cnpj_basico = e.cnpj_basico
		%s`, where)
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres.searchViaSocios count: %w", err)
	}

	args = append(args, limit, offset)
	dataQ := baseSelectQuery() + "\n" + where +
		fmt.Sprintf("\nORDER BY e.razao_social, est.cnpj_ordem\nLIMIT $%d OFFSET $%d", n, n+1)

	rows, err := r.db.Query(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres.searchViaSocios: %w", err)
	}
	defer rows.Close()

	results, err := scanEmpresas(ctx, r.db, rows, true)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// searchViaSociosCPF usa índice de igualdade no CPF mascarado (sem COUNT global).
func (r *empresaRepository) searchViaSociosCPF(ctx context.Context, f model.SearchFilter, cpf string) ([]*model.EmpresaResult, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	basicos, err := r.distinctBasicosByCPF(ctx, cpf)
	if err != nil {
		return nil, 0, err
	}
	if len(basicos) == 0 {
		return []*model.EmpresaResult{}, 0, nil
	}
	return r.fetchEstabelecimentosForBasicos(ctx, f, basicos)
}

// searchViaSociosCPFAndNome: índice de igualdade no CPF, filtro de nome em memória (poucas linhas).
func (r *empresaRepository) searchViaSociosCPFAndNome(ctx context.Context, f model.SearchFilter, cpf string) ([]*model.EmpresaResult, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	masked, digits := cpfMasked(cpf)
	nomeNeedle := strings.ToUpper(f.NomeSocio)

	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT s.cnpj_basico,
		       COALESCE(s.nome_socio, ''),
		       COALESCE(s.nome_do_representante, '')
		FROM cnpj.socios s
		WHERE s.cnpj_cpf_do_socio = $1
		   OR s.representante_legal = $1
		   OR s.representante_legal LIKE '%' || $2 || '%'`, masked, digits)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres.searchViaSociosCPFAndNome: %w", err)
	}
	defer rows.Close()

	basicoSet := make(map[string]struct{})
	for rows.Next() {
		var basico, nomeSocio, nomeRep string
		if err := rows.Scan(&basico, &nomeSocio, &nomeRep); err != nil {
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
	if len(basicos) == 0 {
		return []*model.EmpresaResult{}, 0, nil
	}
	return r.fetchEstabelecimentosForBasicos(ctx, f, basicos)
}

func (r *empresaRepository) distinctBasicosByCPF(ctx context.Context, cpf string) ([]string, error) {
	masked, digits := cpfMasked(cpf)
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT s.cnpj_basico
		FROM cnpj.socios s
		WHERE s.cnpj_cpf_do_socio = $1
		   OR s.representante_legal = $1
		   OR s.representante_legal LIKE '%' || $2 || '%'`, masked, digits)
	if err != nil {
		return nil, fmt.Errorf("distinctBasicosByCPF: %w", err)
	}
	defer rows.Close()
	return scanDistinctBasicos(rows)
}

func scanDistinctBasicos(rows pgx.Rows) ([]string, error) {
	var basicos []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		basicos = append(basicos, b)
	}
	return basicos, rows.Err()
}

func cpfMasked(cpf string) (masked, digits string) {
	digits = cpf
	if len(cpf) == 11 {
		digits = cpf[3:9]
	}
	return "***" + digits + "**", digits
}

// fetchEstabelecimentosForBasicos: uma única query com detalhe + paginação.
func (r *empresaRepository) fetchEstabelecimentosForBasicos(ctx context.Context, f model.SearchFilter, basicos []string) ([]*model.EmpresaResult, int, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	args := []any{basicos}
	conditions := []string{"e.cnpj_basico = ANY($1)"}
	n := 2

	if f.UF != "" {
		conditions = append(conditions, fmt.Sprintf("est.uf = $%d", n))
		args = append(args, strings.ToUpper(f.UF))
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
	if f.Municipio != "" {
		conditions = append(conditions, fmt.Sprintf(`est.municipio IN (
			SELECT codigo FROM cnpj.municipios WHERE descricao ILIKE '%%' || $%d || '%%'
		)`, n))
		args = append(args, f.Municipio)
		n++
	}
	if f.Porte != "" {
		conditions = append(conditions, fmt.Sprintf("e.porte = $%d", n))
		args = append(args, f.Porte)
		n++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit, offset)

	q := baseSelectQuery() + "\n" + where +
		fmt.Sprintf("\nORDER BY e.razao_social, est.cnpj_ordem\nLIMIT $%d OFFSET $%d", n, n+1)

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("fetchEstabelecimentosForBasicos: %w", err)
	}
	defer rows.Close()

	results, err := scanEmpresas(ctx, r.db, rows, true)
	if err != nil {
		return nil, 0, err
	}

	total := offset + len(results)
	if len(results) >= limit || page > 1 {
		if t, err := r.countEstabelecimentosForBasicos(ctx, f, basicos); err == nil {
			total = t
		}
	}
	return results, total, nil
}

func (r *empresaRepository) countEstabelecimentosForBasicos(ctx context.Context, f model.SearchFilter, basicos []string) (int, error) {
	args := []any{basicos}
	conditions := []string{"est.cnpj_basico = ANY($1)"}
	n := 2
	if f.UF != "" {
		conditions = append(conditions, fmt.Sprintf("est.uf = $%d", n))
		args = append(args, strings.ToUpper(f.UF))
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
	if f.Municipio != "" {
		conditions = append(conditions, fmt.Sprintf(`est.municipio IN (
			SELECT codigo FROM cnpj.municipios WHERE descricao ILIKE '%%' || $%d || '%%'
		)`, n))
		args = append(args, f.Municipio)
		n++
	}
	if f.Porte != "" {
		conditions = append(conditions, fmt.Sprintf(`est.cnpj_basico IN (
			SELECT cnpj_basico FROM cnpj.empresas WHERE porte = $%d
		)`, n))
		args = append(args, f.Porte)
	}
	var total int
	err := r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM cnpj.estabelecimentos est
		JOIN cnpj.empresas e ON e.cnpj_basico = est.cnpj_basico
		WHERE %s`, strings.Join(conditions, " AND ")), args...).Scan(&total)
	return total, err
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
		COALESCE(qs_resp.descricao, ''),
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
	LEFT JOIN cnpj.qualificacoes_socios qs_resp ON qs_resp.codigo = e.qualificacao_responsavel
	LEFT JOIN cnpj.municipios mun           ON mun.codigo = est.municipio
	LEFT JOIN cnpj.motivos mot              ON mot.codigo = est.motivo_situacao_cadastral
	LEFT JOIN cnpj.cnaes cn                ON cn.codigo = est.cnae_fiscal_principal
	LEFT JOIN cnpj.paises pai              ON pai.codigo = est.pais
	LEFT JOIN cnpj.dados_simples ds        ON ds.cnpj_basico = e.cnpj_basico`
}

func parseCNAECodes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var codes []string
	for _, part := range strings.Split(raw, ",") {
		var digits strings.Builder
		for _, r := range strings.TrimSpace(part) {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
			if digits.Len() == 7 {
				break
			}
		}
		if digits.Len() != 7 {
			continue
		}
		c := digits.String()
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		codes = append(codes, c)
	}
	return codes
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
