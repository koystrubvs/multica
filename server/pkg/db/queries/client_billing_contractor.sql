-- client_billing_contractor.sql — contractor-level billing settings and the
-- period lookups that back consolidated per-contractor invoicing (migration
-- 202). NUMERIC columns cross the boundary as float8, same convention as
-- client_billing.sql / client_billing_period.sql.

-- name: GetClientBillingContractorConfig :one
SELECT workspace_id, elba_contractor_id, name, mode,
       COALESCE(subscription_fee_rub, 0)::float8 AS subscription_fee_rub,
       elba_bank_account_id, created_at, updated_at
FROM client_billing_contractor_config
WHERE workspace_id = @workspace_id AND elba_contractor_id = @elba_contractor_id;

-- name: ListClientBillingContractorConfigs :many
SELECT workspace_id, elba_contractor_id, name, mode,
       COALESCE(subscription_fee_rub, 0)::float8 AS subscription_fee_rub,
       elba_bank_account_id, created_at, updated_at
FROM client_billing_contractor_config
WHERE workspace_id = @workspace_id
ORDER BY name NULLS LAST, elba_contractor_id;

-- name: UpsertClientBillingContractorConfig :one
INSERT INTO client_billing_contractor_config (
    workspace_id, elba_contractor_id, name, mode,
    subscription_fee_rub, elba_bank_account_id, updated_at
) VALUES (
    @workspace_id, @elba_contractor_id,
    sqlc.narg('name'), @mode,
    sqlc.narg('subscription_fee_rub')::float8,
    sqlc.narg('elba_bank_account_id'),
    now()
)
ON CONFLICT (workspace_id, elba_contractor_id) DO UPDATE SET
    name = EXCLUDED.name,
    mode = EXCLUDED.mode,
    subscription_fee_rub = EXCLUDED.subscription_fee_rub,
    elba_bank_account_id = EXCLUDED.elba_bank_account_id,
    updated_at = now()
RETURNING workspace_id, elba_contractor_id, name, mode,
       COALESCE(subscription_fee_rub, 0)::float8 AS subscription_fee_rub,
       elba_bank_account_id, created_at, updated_at;

-- name: ListInvoiceableClosedPeriodsInWorkspace :many
-- Every closed (not yet invoiced) period in the workspace whose project is
-- linked to an Elba contractor, tagged with that contractor id and the project
-- title. The UI groups these by (contractor, cycle) to offer one "Выставить
-- счёт" button per group.
SELECT p.id, p.project_id, p.workspace_id, p.starts_on, p.ends_on, p.status,
       p.total_rub::float8 AS total_rub, p.elba_invoice_id, p.elba_act_id,
       cfg.elba_contractor_id, pr.title AS project_title
FROM client_billing_period p
JOIN client_billing_config cfg ON cfg.project_id = p.project_id
JOIN project pr ON pr.id = p.project_id
WHERE p.workspace_id = @workspace_id
  AND p.status = 'closed'
  AND cfg.elba_contractor_id IS NOT NULL
ORDER BY p.starts_on DESC, pr.title;

-- name: ListClosedPeriodsForContractorCycle :many
-- The closed, uninvoiced periods that make up one contractor invoice: all
-- projects sharing @elba_contractor_id with the exact same billing cycle
-- (matching starts_on AND ends_on). project_title drives the per-project line
-- grouping in the consolidated bill.
SELECT p.id, p.project_id, p.workspace_id, p.starts_on, p.ends_on, p.status,
       p.total_rub::float8 AS total_rub, pr.title AS project_title
FROM client_billing_period p
JOIN client_billing_config cfg ON cfg.project_id = p.project_id
JOIN project pr ON pr.id = p.project_id
WHERE p.workspace_id = @workspace_id
  AND cfg.elba_contractor_id = @elba_contractor_id
  AND p.starts_on = @starts_on AND p.ends_on = @ends_on
  AND p.status = 'closed'
ORDER BY pr.title;
