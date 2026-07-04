-- 134: client billing v2 — metering (spec §10, multica/AGENCY_BILLING_SPEC.md).
--
-- Charges become "issue × period" lines instead of one-per-issue done-snapshots:
-- the sweep (manual or at period close) bills the delta between an issue's
-- cumulative usage and the sum of its previous charge snapshots — the charge
-- ledger itself is the watermark, and void rows advance it exactly like
-- confirmed ones so excluded tokens never resurface in a later cycle. The
-- UNIQUE(issue_id) therefore goes away.
--
-- `source` records where a line came from; every pre-v2 row was created by the
-- (now removed) done-transition hook.
--
-- client_billing_dispute lets a client (member or SitePing guest) contest an
-- issue's accrued cost before it is invoiced; open disputes block period close.

ALTER TABLE client_billing_charge DROP CONSTRAINT IF EXISTS client_billing_charge_issue_id_key;
CREATE INDEX IF NOT EXISTS idx_client_billing_charge_issue
    ON client_billing_charge(issue_id, created_at DESC);

ALTER TABLE client_billing_charge ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'sweep'
    CHECK (source IN ('done_hook','sweep','manual'));
UPDATE client_billing_charge SET source = 'done_hook';

CREATE TABLE IF NOT EXISTS client_billing_dispute (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id           UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    project_id         UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    workspace_id       UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    opened_by_type     TEXT NOT NULL CHECK (opened_by_type IN ('member','guest')),
    -- user id for members, siteping guest member id for guests; nullable so a
    -- deleted actor doesn't orphan the audit row.
    opened_by          UUID,
    reason             TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
    resolution         TEXT CHECK (resolution IN ('keep','exclude','adjust')),
    resolution_comment TEXT,
    resolved_by        UUID REFERENCES "user"(id) ON DELETE SET NULL,
    resolved_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One live dispute per issue; also serves the open-dispute lookup.
CREATE UNIQUE INDEX IF NOT EXISTS uq_client_billing_dispute_issue_open
    ON client_billing_dispute(issue_id) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_client_billing_dispute_project
    ON client_billing_dispute(project_id, status);
