-- client_billing.sql — agency-side client billing (see migration 120).
-- All NUMERIC columns are cast to/from float8 at the query boundary so the
-- generated Go code works in float64 instead of pgtype.Numeric. Precision is
-- fine for this domain: prices are rubles rounded to a 50₽ step.

-- name: GetClientBillingConfig :one
SELECT
    project_id, enabled, mode,
    markup::float8            AS markup,
    min_price_rub::float8     AS min_price_rub,
    rounding_rub::float8      AS rounding_rub,
    fx_markup_percent::float8 AS fx_markup_percent,
    COALESCE(budget_rub, 0)::float8 AS budget_rub,
    COALESCE(subscription_fee_rub, 0)::float8 AS subscription_fee_rub,
    COALESCE(fair_use_rub, 0)::float8 AS fair_use_rub,
    period_months, anchor_day, created_at, updated_at
FROM client_billing_config
WHERE project_id = @project_id;

-- name: UpsertClientBillingConfig :one
INSERT INTO client_billing_config (
    project_id, enabled, mode, markup, min_price_rub, rounding_rub,
    fx_markup_percent, budget_rub, subscription_fee_rub, fair_use_rub,
    period_months, anchor_day, updated_at
) VALUES (
    @project_id, @enabled, @mode,
    @markup::float8, @min_price_rub::float8, @rounding_rub::float8,
    @fx_markup_percent::float8,
    sqlc.narg('budget_rub')::float8,
    sqlc.narg('subscription_fee_rub')::float8,
    sqlc.narg('fair_use_rub')::float8,
    @period_months, @anchor_day, now()
)
ON CONFLICT (project_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    mode = EXCLUDED.mode,
    markup = EXCLUDED.markup,
    min_price_rub = EXCLUDED.min_price_rub,
    rounding_rub = EXCLUDED.rounding_rub,
    fx_markup_percent = EXCLUDED.fx_markup_percent,
    budget_rub = EXCLUDED.budget_rub,
    subscription_fee_rub = EXCLUDED.subscription_fee_rub,
    fair_use_rub = EXCLUDED.fair_use_rub,
    period_months = EXCLUDED.period_months,
    anchor_day = EXCLUDED.anchor_day,
    updated_at = now()
RETURNING
    project_id, enabled, mode,
    markup::float8            AS markup,
    min_price_rub::float8     AS min_price_rub,
    rounding_rub::float8      AS rounding_rub,
    fx_markup_percent::float8 AS fx_markup_percent,
    COALESCE(budget_rub, 0)::float8 AS budget_rub,
    COALESCE(subscription_fee_rub, 0)::float8 AS subscription_fee_rub,
    COALESCE(fair_use_rub, 0)::float8 AS fair_use_rub,
    period_months, anchor_day, created_at, updated_at;

-- name: ListIssueUsageByModel :many
-- Per-(provider, model) token totals for every task of an issue. This is the
-- raw input for the no-cache price snapshot; the Go layer applies the
-- per-model price table.
SELECT
    tu.provider,
    tu.model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id = @issue_id
GROUP BY tu.provider, tu.model
ORDER BY tu.provider, tu.model;

-- name: CreateClientBillingCharge :one
INSERT INTO client_billing_charge (
    issue_id, project_id, workspace_id, usage,
    nocache_usd, fx_rate, markup, price_rub
) VALUES (
    @issue_id, @project_id, @workspace_id, @usage,
    @nocache_usd::float8, @fx_rate::float8, @markup::float8, @price_rub::float8
)
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: GetClientBillingChargeByIssue :one
SELECT
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at
FROM client_billing_charge
WHERE issue_id = @issue_id;

-- name: ConfirmClientBillingCharge :one
UPDATE client_billing_charge
SET status = 'confirmed', confirmed_by = @user_id, confirmed_at = now(), updated_at = now()
WHERE issue_id = @issue_id AND status = 'draft'
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: VoidClientBillingCharge :one
UPDATE client_billing_charge
SET status = 'void', updated_at = now()
WHERE issue_id = @issue_id AND status <> 'void'
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: AdjustClientBillingCharge :one
-- Manual price override while still draft; adjusted_reason is the audit trail.
UPDATE client_billing_charge
SET price_rub = @price_rub::float8, adjusted_reason = @reason, updated_at = now()
WHERE issue_id = @issue_id AND status = 'draft'
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: ListClientBillingChargesByProject :many
SELECT
    cbc.id, cbc.issue_id, cbc.project_id, cbc.workspace_id, cbc.period_id, cbc.usage,
    cbc.nocache_usd::float8 AS nocache_usd,
    cbc.fx_rate::float8     AS fx_rate,
    cbc.markup::float8      AS markup,
    cbc.price_rub::float8   AS price_rub,
    cbc.status, cbc.adjusted_reason, cbc.confirmed_by, cbc.confirmed_at,
    cbc.created_at, cbc.updated_at,
    i.title AS issue_title
FROM client_billing_charge cbc
JOIN issue i ON i.id = cbc.issue_id
WHERE cbc.project_id = @project_id
  AND (sqlc.narg('status')::text IS NULL OR cbc.status = sqlc.narg('status'))
ORDER BY cbc.created_at DESC;
