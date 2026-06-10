-- 121: client billing periods — the invoicing container for confirmed
-- charges (phase 2 of the agency billing plan; see 120 for the core model).
--
-- A period is one billing cycle of a project (anchor_day + period_months from
-- client_billing_config). Confirmed charges attach to the open period; closing
-- freezes the total that goes to the invoice. Budget alerts (mode=budget) and
-- fair-use alerts (mode=subscription) are tracked per period via
-- last_alert_percent so each 25/50/75/100% threshold fires exactly once.

CREATE TABLE IF NOT EXISTS client_billing_period (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    workspace_id       UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    starts_on          DATE NOT NULL,
    -- exclusive upper bound: a charge confirmed on ends_on belongs to the next period
    ends_on            DATE NOT NULL,
    status             TEXT NOT NULL DEFAULT 'open'
                       CHECK (status IN ('open','closed','invoiced','paid')),
    total_rub          NUMERIC(14,2) NOT NULL DEFAULT 0,
    last_alert_percent INT NOT NULL DEFAULT 0,
    elba_invoice_id    TEXT,
    elba_act_id        TEXT,
    report_file        TEXT,
    closed_at          TIMESTAMPTZ,
    paid_at            TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, starts_on)
);

CREATE INDEX IF NOT EXISTS idx_client_billing_period_project
    ON client_billing_period(project_id, status, starts_on DESC);

ALTER TABLE client_billing_charge
    ADD COLUMN IF NOT EXISTS period_id UUID REFERENCES client_billing_period(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_client_billing_charge_period
    ON client_billing_charge(period_id);
