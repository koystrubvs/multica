-- client_billing_period.sql — billing cycles for the agency client billing
-- domain (migration 121). NUMERIC columns cross the boundary as float8, same
-- convention as client_billing.sql.

-- name: GetOpenClientBillingPeriod :one
SELECT id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at
FROM client_billing_period
WHERE project_id = @project_id AND status = 'open'
ORDER BY starts_on DESC
LIMIT 1;

-- name: GetClientBillingPeriodInProject :one
SELECT id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at
FROM client_billing_period
WHERE id = @id AND project_id = @project_id;

-- name: CreateClientBillingPeriod :one
INSERT INTO client_billing_period (project_id, workspace_id, starts_on, ends_on)
VALUES (@project_id, @workspace_id, @starts_on, @ends_on)
ON CONFLICT (project_id, starts_on) DO UPDATE SET updated_at = now()
RETURNING id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at;

-- name: ListClientBillingPeriods :many
SELECT id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at
FROM client_billing_period
WHERE project_id = @project_id
ORDER BY starts_on DESC;

-- name: AttachChargeToPeriod :exec
UPDATE client_billing_charge
SET period_id = @period_id, updated_at = now()
WHERE id = @charge_id;

-- name: SumConfirmedChargesInPeriod :one
SELECT COALESCE(SUM(price_rub), 0)::float8 AS total
FROM client_billing_charge
WHERE period_id = @period_id AND status = 'confirmed';

-- name: UpdateClientBillingPeriodTotal :exec
UPDATE client_billing_period
SET total_rub = @total_rub::float8, updated_at = now()
WHERE id = @id;

-- name: SetClientBillingPeriodAlertPercent :exec
-- Monotonic: never lowers the watermark, so a threshold alerts only once
-- even when two confirms race.
UPDATE client_billing_period
SET last_alert_percent = @percent, updated_at = now()
WHERE id = @id AND last_alert_percent < @percent;

-- name: MarkClientBillingPeriodReady :one
-- Freeze preview total and flip open -> ready when the cycle has elapsed.
-- New work continues in a successor open period.
UPDATE client_billing_period
SET status = 'ready', total_rub = @total_rub::float8, updated_at = now()
WHERE id = @id AND status = 'open'
RETURNING id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at;

-- name: CloseClientBillingPeriod :one
-- Accepts open (legacy / early close) or ready (reviewed package).
UPDATE client_billing_period
SET status = 'closed', total_rub = @total_rub::float8, closed_at = now(), updated_at = now()
WHERE id = @id AND status IN ('open', 'ready')
RETURNING id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at;

-- name: ListBusinessBillingRunPeriods :many
-- Periods for the Business billing queue: open (upcoming), ready (review),
-- and recently invoiced. Scoped to projects mapped into this business.
SELECT per.id, per.project_id, per.workspace_id, per.starts_on, per.ends_on, per.status,
       per.total_rub::float8 AS total_rub, per.last_alert_percent,
       per.elba_invoice_id, per.elba_act_id, per.report_file,
       per.closed_at, per.paid_at, per.created_at, per.updated_at,
       p.title AS project_title,
       cfg.elba_contractor_id,
       cfg.mode AS billing_mode,
       cfg.anchor_day,
       cfg.enabled AS billing_enabled,
       bcp.client_id,
       bc.canonical_name AS client_name,
       COALESCE((
         SELECT COUNT(*)::int FROM client_billing_charge c
         WHERE c.period_id = per.id AND c.status <> 'void'
       ), 0)::int AS charge_count,
       COALESCE((
         SELECT SUM(c.price_rub)::float8 FROM client_billing_charge c
         WHERE c.period_id = per.id AND c.status = 'confirmed'
       ), 0)::float8 AS confirmed_total_rub,
       COALESCE((
         SELECT COUNT(*)::int FROM client_billing_charge c
         WHERE c.period_id = per.id AND c.status = 'draft'
       ), 0)::int AS draft_count
FROM client_billing_period per
JOIN project p ON p.id = per.project_id
JOIN client_billing_config cfg ON cfg.project_id = per.project_id AND cfg.enabled
JOIN business_client_project bcp ON bcp.project_id = per.project_id AND bcp.business_id = @business_id
LEFT JOIN business_client bc ON bc.id = bcp.client_id
WHERE per.status IN ('open', 'ready', 'closed', 'invoiced')
  AND (
    per.status IN ('open', 'ready', 'closed')
    OR per.updated_at >= now() - interval '90 days'
  )
ORDER BY
  CASE per.status
    WHEN 'ready' THEN 0
    WHEN 'closed' THEN 1
    WHEN 'open' THEN 2
    ELSE 3
  END,
  per.ends_on ASC,
  p.title ASC;

-- name: ReopenClientBillingPeriod :one
UPDATE client_billing_period
SET status = 'open', closed_at = NULL, paid_at = NULL, updated_at = now()
WHERE id = @id AND status IN ('closed', 'invoiced')
RETURNING id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at;

-- name: MarkClientBillingPeriodPaid :one
UPDATE client_billing_period
SET status = 'paid', paid_at = now(), updated_at = now()
WHERE id = @id AND status IN ('closed', 'invoiced')
RETURNING id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at;

-- name: ListClientBillingChargesByPeriod :many
SELECT cbc.id, cbc.issue_id, cbc.project_id, cbc.workspace_id, cbc.period_id, cbc.usage,
       cbc.nocache_usd::float8 AS nocache_usd,
       cbc.fx_rate::float8     AS fx_rate,
       cbc.markup::float8      AS markup,
       cbc.price_rub::float8   AS price_rub,
       cbc.status, cbc.source, cbc.adjusted_reason, cbc.confirmed_by, cbc.confirmed_at,
       cbc.created_at, cbc.updated_at,
       i.title AS issue_title
FROM client_billing_charge cbc
JOIN issue i ON i.id = cbc.issue_id
WHERE cbc.period_id = @period_id
ORDER BY cbc.confirmed_at DESC NULLS LAST, cbc.created_at DESC;

-- name: CountDraftChargesInProject :one
SELECT COUNT(*)::int FROM client_billing_charge
WHERE project_id = @project_id AND status = 'draft';

-- name: SetClientBillingPeriodElba :one
-- Records the Elba documents created for the period and flips it to
-- `invoiced`. Allowed from `closed` (normal flow: close -> push).
UPDATE client_billing_period
SET status = 'invoiced', elba_invoice_id = @invoice_id, elba_act_id = @act_id,
    report_file = sqlc.narg('report_file'), updated_at = now()
WHERE id = @id AND status = 'closed'
RETURNING id, project_id, workspace_id, starts_on, ends_on, status,
       total_rub::float8 AS total_rub, last_alert_percent,
       elba_invoice_id, elba_act_id, report_file,
       closed_at, paid_at, created_at, updated_at;
