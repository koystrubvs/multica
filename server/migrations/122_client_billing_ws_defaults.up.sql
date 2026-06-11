-- 122: workspace-level billing defaults + Elba wiring (phase 3 groundwork).
--
-- Pricing knobs (markup K, fx markup, floor, rounding) move to a per-workspace
-- defaults table; the per-project columns become NULLable where NULL means
-- "inherit the workspace default". Existing project rows that carried the old
-- hardcoded defaults are nulled out so they start inheriting.
--
-- Elba (Kontur Elba) identifiers: the workspace stores the organization and
-- default bank account; a project links a contractor (already present from
-- migration 120) and may override the bank account.

CREATE TABLE IF NOT EXISTS client_billing_workspace_config (
    workspace_id         UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    markup               NUMERIC(8,3)  NOT NULL DEFAULT 3.0   CHECK (markup > 0),
    min_price_rub        NUMERIC(12,2) NOT NULL DEFAULT 500   CHECK (min_price_rub >= 0),
    rounding_rub         NUMERIC(12,2) NOT NULL DEFAULT 50    CHECK (rounding_rub >= 0),
    fx_markup_percent    NUMERIC(6,3)  NOT NULL DEFAULT 5.0,
    elba_org_id          TEXT,
    elba_bank_account_id TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE client_billing_config
    ALTER COLUMN markup DROP NOT NULL,
    ALTER COLUMN markup DROP DEFAULT,
    ALTER COLUMN min_price_rub DROP NOT NULL,
    ALTER COLUMN min_price_rub DROP DEFAULT,
    ALTER COLUMN rounding_rub DROP NOT NULL,
    ALTER COLUMN rounding_rub DROP DEFAULT,
    ALTER COLUMN fx_markup_percent DROP NOT NULL,
    ALTER COLUMN fx_markup_percent DROP DEFAULT;

-- Rows that still carry the old hardcoded defaults switch to inheriting.
UPDATE client_billing_config SET markup = NULL            WHERE markup = 3.0;
UPDATE client_billing_config SET min_price_rub = NULL     WHERE min_price_rub = 500;
UPDATE client_billing_config SET rounding_rub = NULL      WHERE rounding_rub = 50;
UPDATE client_billing_config SET fx_markup_percent = NULL WHERE fx_markup_percent = 5.0;

ALTER TABLE client_billing_config
    ADD COLUMN IF NOT EXISTS elba_contractor_id TEXT,
    ADD COLUMN IF NOT EXISTS elba_bank_account_id TEXT;
