-- 202: contractor-level billing settings for consolidated invoicing.
--
-- A single Elba contractor (elba_contractor_id) can be linked to several
-- projects (e.g. «Долголетие» = spina.spb.ru + plastika.me). Project configs
-- (client_billing_config) stay the metering source — how each project accrues
-- charges — but the *invoice* is issued per contractor: one счёт + акт covering
-- every project's closed period for the same cycle. The subscription cap
-- (fixed monthly fee) therefore moves up here, applying to the contractor as a
-- whole instead of per project.
--
-- Keyed by (workspace_id, elba_contractor_id): a contractor id is unique
-- within an Elba organization, which is workspace-scoped. `mode` mirrors the
-- project-level enum but only postpaid / subscription are meaningful for the
-- consolidated bill (budget is a per-project alerting mode).

CREATE TABLE IF NOT EXISTS client_billing_contractor_config (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    elba_contractor_id   TEXT NOT NULL,
    -- Display label cached from Elba so the settings UI need not re-fetch the
    -- contractor list to show a name; purely cosmetic.
    name                 TEXT,
    mode                 TEXT NOT NULL DEFAULT 'postpaid'
                         CHECK (mode IN ('postpaid','subscription')),
    subscription_fee_rub NUMERIC(14,2),
    -- Optional override of the workspace default bank account for this
    -- contractor's bills.
    elba_bank_account_id TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, elba_contractor_id)
);
