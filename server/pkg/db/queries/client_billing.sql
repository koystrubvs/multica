-- client_billing.sql — agency-side client billing (see migrations 120/122).
-- All NUMERIC columns are cast to/from float8 at the query boundary so the
-- generated Go code works in float64 instead of pgtype.Numeric. Precision is
-- fine for this domain: prices are rubles rounded to a 50-RUB step.
--
-- Since migration 122 the four pricing knobs (markup, min_price_rub,
-- rounding_rub, fx_markup_percent) are NULLable on the project config — NULL
-- means "inherit the workspace default". sqlc can't express nullable float8
-- outputs cleanly, so each knob crosses the boundary as a (value, *_set)
-- pair: COALESCE(x, 0) + (x IS NOT NULL).

-- name: GetClientBillingConfig :one
SELECT
    project_id, enabled, mode,
    (markup IS NOT NULL)::bool            AS markup_set,
    COALESCE(markup, 0)::float8           AS markup,
    (min_price_rub IS NOT NULL)::bool     AS min_price_rub_set,
    COALESCE(min_price_rub, 0)::float8    AS min_price_rub,
    (rounding_rub IS NOT NULL)::bool      AS rounding_rub_set,
    COALESCE(rounding_rub, 0)::float8     AS rounding_rub,
    (fx_markup_percent IS NOT NULL)::bool AS fx_markup_percent_set,
    COALESCE(fx_markup_percent, 0)::float8 AS fx_markup_percent,
    COALESCE(budget_rub, 0)::float8 AS budget_rub,
    COALESCE(subscription_fee_rub, 0)::float8 AS subscription_fee_rub,
    COALESCE(fair_use_rub, 0)::float8 AS fair_use_rub,
    period_months, anchor_day,
    elba_contractor_id, elba_bank_account_id,
    created_at, updated_at
FROM client_billing_config
WHERE project_id = @project_id;

-- name: UpsertClientBillingConfig :one
INSERT INTO client_billing_config (
    project_id, enabled, mode, markup, min_price_rub, rounding_rub,
    fx_markup_percent, budget_rub, subscription_fee_rub, fair_use_rub,
    period_months, anchor_day, elba_contractor_id, elba_bank_account_id,
    updated_at
) VALUES (
    @project_id, @enabled, @mode,
    sqlc.narg('markup')::float8,
    sqlc.narg('min_price_rub')::float8,
    sqlc.narg('rounding_rub')::float8,
    sqlc.narg('fx_markup_percent')::float8,
    sqlc.narg('budget_rub')::float8,
    sqlc.narg('subscription_fee_rub')::float8,
    sqlc.narg('fair_use_rub')::float8,
    @period_months, @anchor_day,
    sqlc.narg('elba_contractor_id'),
    sqlc.narg('elba_bank_account_id'),
    now()
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
    elba_contractor_id = EXCLUDED.elba_contractor_id,
    elba_bank_account_id = EXCLUDED.elba_bank_account_id,
    updated_at = now()
RETURNING
    project_id, enabled, mode,
    (markup IS NOT NULL)::bool            AS markup_set,
    COALESCE(markup, 0)::float8           AS markup,
    (min_price_rub IS NOT NULL)::bool     AS min_price_rub_set,
    COALESCE(min_price_rub, 0)::float8    AS min_price_rub,
    (rounding_rub IS NOT NULL)::bool      AS rounding_rub_set,
    COALESCE(rounding_rub, 0)::float8     AS rounding_rub,
    (fx_markup_percent IS NOT NULL)::bool AS fx_markup_percent_set,
    COALESCE(fx_markup_percent, 0)::float8 AS fx_markup_percent,
    COALESCE(budget_rub, 0)::float8 AS budget_rub,
    COALESCE(subscription_fee_rub, 0)::float8 AS subscription_fee_rub,
    COALESCE(fair_use_rub, 0)::float8 AS fair_use_rub,
    period_months, anchor_day,
    elba_contractor_id, elba_bank_account_id,
    created_at, updated_at;

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
-- Metering (migration 134): a charge is one "issue × period" line; several may
-- exist per issue. period_id is set immediately for sweep-created lines and
-- stays NULL for legacy/manual drafts until confirm attaches them.
INSERT INTO client_billing_charge (
    issue_id, project_id, workspace_id, usage,
    nocache_usd, fx_rate, markup, price_rub, source, period_id
) VALUES (
    @issue_id, @project_id, @workspace_id, @usage,
    @nocache_usd::float8, @fx_rate::float8, @markup::float8, @price_rub::float8,
    @source, sqlc.narg('period_id')::uuid
)
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, source, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: GetClientBillingChargeByIssue :one
-- Latest charge of an issue (the ledger head).
SELECT
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, source, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at
FROM client_billing_charge
WHERE issue_id = @issue_id
ORDER BY created_at DESC
LIMIT 1;

-- name: ListClientBillingChargesByIssue :many
SELECT
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, source, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at
FROM client_billing_charge
WHERE issue_id = @issue_id
ORDER BY created_at DESC;

-- name: ListChargeUsageByIssue :many
-- Usage snapshots of ALL charges (void included): the issue's billed-through
-- watermark is the per-class sum of these rows.
SELECT usage FROM client_billing_charge WHERE issue_id = @issue_id;

-- name: ListChargeUsageByProject :many
SELECT issue_id, usage FROM client_billing_charge WHERE project_id = @project_id;

-- name: ConfirmClientBillingCharge :one
UPDATE client_billing_charge
SET status = 'confirmed', confirmed_by = @user_id, confirmed_at = now(), updated_at = now()
WHERE id = @charge_id AND issue_id = @issue_id AND status = 'draft'
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, source, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: ConfirmDraftChargesInPeriod :many
-- Period close auto-confirms whatever drafts are still attached: the review
-- happens in the period charge list BEFORE close (void/adjust the outliers).
UPDATE client_billing_charge
SET status = 'confirmed', confirmed_by = @user_id, confirmed_at = now(), updated_at = now()
WHERE period_id = @period_id AND status = 'draft'
RETURNING id;

-- name: VoidClientBillingCharge :one
UPDATE client_billing_charge
SET status = 'void', updated_at = now()
WHERE id = @charge_id AND issue_id = @issue_id AND status <> 'void'
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, source, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: VoidDraftChargesByIssue :many
-- Dispute resolution "exclude": every pending draft of the issue is voided in
-- one shot (void rows keep advancing the watermark).
UPDATE client_billing_charge
SET status = 'void', updated_at = now()
WHERE issue_id = @issue_id AND status = 'draft'
RETURNING id, period_id;

-- name: AdjustClientBillingCharge :one
-- Manual price override while still draft; adjusted_reason is the audit trail.
UPDATE client_billing_charge
SET price_rub = @price_rub::float8, adjusted_reason = @reason, updated_at = now()
WHERE id = @charge_id AND issue_id = @issue_id AND status = 'draft'
RETURNING
    id, issue_id, project_id, workspace_id, period_id, usage,
    nocache_usd::float8 AS nocache_usd,
    fx_rate::float8     AS fx_rate,
    markup::float8      AS markup,
    price_rub::float8   AS price_rub,
    status, source, adjusted_reason, confirmed_by, confirmed_at, created_at, updated_at;

-- name: ListClientBillingChargesByProject :many
SELECT
    cbc.id, cbc.issue_id, cbc.project_id, cbc.workspace_id, cbc.period_id, cbc.usage,
    cbc.nocache_usd::float8 AS nocache_usd,
    cbc.fx_rate::float8     AS fx_rate,
    cbc.markup::float8      AS markup,
    cbc.price_rub::float8   AS price_rub,
    cbc.status, cbc.source, cbc.adjusted_reason, cbc.confirmed_by, cbc.confirmed_at,
    cbc.created_at, cbc.updated_at,
    i.title AS issue_title
FROM client_billing_charge cbc
JOIN issue i ON i.id = cbc.issue_id
WHERE cbc.project_id = @project_id
  AND (sqlc.narg('status')::text IS NULL OR cbc.status = sqlc.narg('status'))
ORDER BY cbc.created_at DESC;

-- name: ListProjectIssueUsageByModel :many
-- Per-issue × (provider, model) token totals for every issue of a project.
-- The sweep diffs these cumulative counters against the charge-ledger
-- watermark to find unbilled deltas.
SELECT
    atq.issue_id,
    tu.provider,
    tu.model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN issue i ON i.id = atq.issue_id
WHERE i.project_id = @project_id
GROUP BY atq.issue_id, tu.provider, tu.model
ORDER BY atq.issue_id, tu.provider, tu.model;

-- --- Disputes (migration 134) ---

-- name: CreateClientBillingDispute :one
INSERT INTO client_billing_dispute (
    issue_id, project_id, workspace_id, opened_by_type, opened_by, reason
) VALUES (
    @issue_id, @project_id, @workspace_id, @opened_by_type, sqlc.narg('opened_by')::uuid, @reason
)
RETURNING id, issue_id, project_id, workspace_id, opened_by_type, opened_by,
    reason, status, resolution, resolution_comment, resolved_by, resolved_at, created_at;

-- name: GetOpenClientBillingDisputeByIssue :one
SELECT id, issue_id, project_id, workspace_id, opened_by_type, opened_by,
    reason, status, resolution, resolution_comment, resolved_by, resolved_at, created_at
FROM client_billing_dispute
WHERE issue_id = @issue_id AND status = 'open';

-- name: ListClientBillingDisputesByProject :many
SELECT cbd.id, cbd.issue_id, cbd.project_id, cbd.workspace_id, cbd.opened_by_type,
    cbd.opened_by, cbd.reason, cbd.status, cbd.resolution, cbd.resolution_comment,
    cbd.resolved_by, cbd.resolved_at, cbd.created_at,
    i.title AS issue_title
FROM client_billing_dispute cbd
JOIN issue i ON i.id = cbd.issue_id
WHERE cbd.project_id = @project_id
  AND (sqlc.narg('status')::text IS NULL OR cbd.status = sqlc.narg('status'))
ORDER BY cbd.created_at DESC;

-- name: ResolveClientBillingDispute :one
UPDATE client_billing_dispute
SET status = 'resolved', resolution = @resolution,
    resolution_comment = sqlc.narg('comment'), resolved_by = @resolved_by, resolved_at = now()
WHERE id = @id AND status = 'open'
RETURNING id, issue_id, project_id, workspace_id, opened_by_type, opened_by,
    reason, status, resolution, resolution_comment, resolved_by, resolved_at, created_at;

-- name: CountOpenClientBillingDisputesInProject :one
SELECT COUNT(*)::int FROM client_billing_dispute
WHERE project_id = @project_id AND status = 'open';

-- name: GetClientBillingWorkspaceConfig :one
SELECT workspace_id,
    markup::float8            AS markup,
    min_price_rub::float8     AS min_price_rub,
    rounding_rub::float8      AS rounding_rub,
    fx_markup_percent::float8 AS fx_markup_percent,
    elba_org_id, elba_bank_account_id,
    created_at, updated_at
FROM client_billing_workspace_config
WHERE workspace_id = @workspace_id;

-- name: UpsertClientBillingWorkspaceConfig :one
INSERT INTO client_billing_workspace_config (
    workspace_id, markup, min_price_rub, rounding_rub, fx_markup_percent,
    elba_org_id, elba_bank_account_id, updated_at
) VALUES (
    @workspace_id,
    @markup::float8, @min_price_rub::float8, @rounding_rub::float8,
    @fx_markup_percent::float8,
    sqlc.narg('elba_org_id'), sqlc.narg('elba_bank_account_id'),
    now()
)
ON CONFLICT (workspace_id) DO UPDATE SET
    markup = EXCLUDED.markup,
    min_price_rub = EXCLUDED.min_price_rub,
    rounding_rub = EXCLUDED.rounding_rub,
    fx_markup_percent = EXCLUDED.fx_markup_percent,
    elba_org_id = EXCLUDED.elba_org_id,
    elba_bank_account_id = EXCLUDED.elba_bank_account_id,
    updated_at = now()
RETURNING workspace_id,
    markup::float8            AS markup,
    min_price_rub::float8     AS min_price_rub,
    rounding_rub::float8      AS rounding_rub,
    fx_markup_percent::float8 AS fx_markup_percent,
    elba_org_id, elba_bank_account_id,
    created_at, updated_at;
