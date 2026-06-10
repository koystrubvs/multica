-- 120: client billing (agency) — per-project billing config + per-issue price
-- snapshots. This is the AGENCY billing domain (what clients owe us for done
-- issues), entirely separate from upstream `cloud_billing` (what we owe the
-- Multica cloud for compute) — hence the client_billing_* prefix.
--
-- Pricing model (see multica/AGENCY_BILLING_SPEC.md in the ops repo):
--   nocache_usd = (input + cache_read + cache_write) * InputPerM
--               + output * OutputPerM            -- public API list prices,
--                                                -- cache discounts NOT passed on
--   price_rub   = round_up(max(nocache_usd * fx_rate * markup, min_price), rounding)
--
-- The charge row is a frozen snapshot taken when the issue transitions to
-- `done`: usage, prices, fx and markup are captured as-of that moment and are
-- never recomputed (invoices must stay stable after the fact).

CREATE TABLE IF NOT EXISTS client_billing_config (
    project_id           UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    enabled              BOOLEAN       NOT NULL DEFAULT true,
    mode                 TEXT          NOT NULL DEFAULT 'postpaid'
                         CHECK (mode IN ('postpaid','budget','subscription')),
    markup               NUMERIC(8,3)  NOT NULL DEFAULT 3.0   CHECK (markup > 0),
    min_price_rub        NUMERIC(12,2) NOT NULL DEFAULT 500   CHECK (min_price_rub >= 0),
    rounding_rub         NUMERIC(12,2) NOT NULL DEFAULT 50    CHECK (rounding_rub >= 0),
    -- USD->RUB = latest CBR rate * (1 + fx_markup_percent/100), frozen into
    -- each charge at snapshot time.
    fx_markup_percent    NUMERIC(6,3)  NOT NULL DEFAULT 5.0,
    -- mode=budget: soft monthly cap used for 25/50/75/100% alerts (phase 2).
    budget_rub           NUMERIC(14,2),
    -- mode=subscription: fixed fee per period + fair-use cap on delivered work.
    subscription_fee_rub NUMERIC(14,2),
    fair_use_rub         NUMERIC(14,2),
    period_months        INT           NOT NULL DEFAULT 1 CHECK (period_months BETWEEN 1 AND 12),
    anchor_day           INT           NOT NULL DEFAULT 1 CHECK (anchor_day BETWEEN 1 AND 28),
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS client_billing_charge (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id        UUID NOT NULL UNIQUE REFERENCES issue(id) ON DELETE CASCADE,
    project_id      UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- Per-(provider, model) token rows as-of snapshot:
    -- [{"provider","model","input_tokens","output_tokens",
    --   "cache_read_tokens","cache_write_tokens","nocache_usd"}]
    usage           JSONB NOT NULL,
    nocache_usd     NUMERIC(14,6) NOT NULL,
    fx_rate         NUMERIC(12,4) NOT NULL,
    markup          NUMERIC(8,3)  NOT NULL,
    price_rub       NUMERIC(14,2) NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft','confirmed','void')),
    -- Manual price override audit trail: NULL means price_rub is the computed
    -- value; non-NULL records why a human changed it.
    adjusted_reason TEXT,
    confirmed_by    UUID REFERENCES "user"(id) ON DELETE SET NULL,
    confirmed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_client_billing_charge_project_status
    ON client_billing_charge(project_id, status);
CREATE INDEX IF NOT EXISTS idx_client_billing_charge_workspace
    ON client_billing_charge(workspace_id, created_at DESC);
