-- W1 Business Control Plane schema. Relationships intentionally have no
-- database foreign keys: workspace remains the native security boundary and
-- application code validates every cross-domain reference.
--
-- Indexes and primary keys are added by later migrations so every index can
-- be built CONCURRENTLY, per the repository migration policy.

CREATE TABLE IF NOT EXISTS business_account (
    id                                  UUID NOT NULL DEFAULT gen_random_uuid(),
    name                                TEXT NOT NULL,
    owner_user_id                       UUID NOT NULL,
    currency                            TEXT NOT NULL DEFAULT 'RUB',
    timezone                            TEXT NOT NULL DEFAULT 'Asia/Yekaterinburg',
    monthly_owner_income_target_rub     NUMERIC(14,2) NOT NULL DEFAULT 1000000,
    created_at                          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_account_name_check
        CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 200),
    CONSTRAINT business_account_currency_check CHECK (currency = 'RUB'),
    CONSTRAINT business_account_timezone_check CHECK (timezone = 'Asia/Yekaterinburg'),
    CONSTRAINT business_account_target_check CHECK (monthly_owner_income_target_rub >= 0)
);

CREATE TABLE IF NOT EXISTS business_account_member (
    business_id     UUID NOT NULL,
    user_id         UUID NOT NULL,
    role            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_account_member_role_check
        CHECK (role IN ('owner', 'finance_admin', 'pm', 'viewer'))
);

CREATE TABLE IF NOT EXISTS business_workspace (
    business_id            UUID NOT NULL,
    workspace_id           UUID NOT NULL,
    kind                   TEXT NOT NULL,
    include_in_portfolio   BOOLEAN NOT NULL DEFAULT true,
    include_revenue        BOOLEAN NOT NULL DEFAULT true,
    include_costs          BOOLEAN NOT NULL DEFAULT true,
    client_id              UUID,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_workspace_kind_check
        CHECK (kind IN ('operational', 'internal', 'client', 'archive')),
    CONSTRAINT business_workspace_client_check
        CHECK (kind = 'client' OR client_id IS NULL),
    CONSTRAINT business_workspace_internal_check
        CHECK (kind <> 'internal' OR include_revenue = false),
    CONSTRAINT business_workspace_archive_check
        CHECK (
            kind <> 'archive'
            OR (
                include_in_portfolio = false
                AND include_revenue = false
                AND include_costs = false
            )
        )
);

CREATE TABLE IF NOT EXISTS business_audit_event (
    id               UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id      UUID NOT NULL,
    actor_user_id    UUID,
    actor_type       TEXT NOT NULL,
    action           TEXT NOT NULL,
    entity_type      TEXT NOT NULL,
    entity_id        UUID,
    request_id       TEXT,
    reason           TEXT,
    before_data      JSONB,
    after_data       JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_audit_event_actor_type_check
        CHECK (actor_type IN ('user', 'system', 'migration')),
    CONSTRAINT business_audit_event_action_check
        CHECK (action = btrim(action) AND char_length(action) BETWEEN 1 AND 120),
    CONSTRAINT business_audit_event_entity_type_check
        CHECK (entity_type = btrim(entity_type) AND char_length(entity_type) BETWEEN 1 AND 120),
    CONSTRAINT business_audit_event_before_data_check
        CHECK (before_data IS NULL OR jsonb_typeof(before_data) = 'object'),
    CONSTRAINT business_audit_event_after_data_check
        CHECK (after_data IS NULL OR jsonb_typeof(after_data) = 'object')
);
