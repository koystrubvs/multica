-- W2-W7 internal business system. Relationships intentionally have no
-- database foreign keys; handlers validate native workspace, project and issue
-- ownership inside the same transaction as every write.
--
-- Primary keys and every other index are created CONCURRENTLY by later,
-- single-statement migrations.

CREATE TABLE IF NOT EXISTS business_client (
    id                       UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id              UUID NOT NULL,
    canonical_name           TEXT NOT NULL,
    status                   TEXT NOT NULL DEFAULT 'prospect',
    manager_user_id          UUID,
    primary_payment_channel  TEXT NOT NULL DEFAULT 'bank',
    notes                    TEXT,
    archived_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_client_name_check
        CHECK (canonical_name = btrim(canonical_name) AND char_length(canonical_name) BETWEEN 1 AND 240),
    CONSTRAINT business_client_status_check
        CHECK (status IN ('prospect', 'active', 'paused', 'leaving', 'lost')),
    CONSTRAINT business_client_payment_channel_check
        CHECK (primary_payment_channel IN ('bank', 'personal_card', 'cash', 'other'))
);

CREATE TABLE IF NOT EXISTS business_client_alias (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id       UUID NOT NULL,
    client_id         UUID NOT NULL,
    source            TEXT NOT NULL,
    alias_type        TEXT NOT NULL,
    value             TEXT NOT NULL,
    normalized_value  TEXT NOT NULL,
    confidence        TEXT NOT NULL DEFAULT 'confirmed',
    auto_match        BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_client_alias_source_check
        CHECK (source IN ('bank', 'elba', 'manual', 'legacy')),
    CONSTRAINT business_client_alias_type_check
        CHECK (alias_type IN ('name', 'inn', 'kpp', 'domain')),
    CONSTRAINT business_client_alias_value_check
        CHECK (value = btrim(value) AND char_length(value) BETWEEN 1 AND 500),
    CONSTRAINT business_client_alias_normalized_check
        CHECK (normalized_value = btrim(normalized_value) AND char_length(normalized_value) BETWEEN 1 AND 500),
    CONSTRAINT business_client_alias_confidence_check
        CHECK (confidence IN ('confirmed', 'needs_review', 'rejected')),
    CONSTRAINT business_client_alias_auto_match_check
        CHECK (auto_match = false OR confidence = 'confirmed')
);

CREATE TABLE IF NOT EXISTS business_client_payer (
    id                    UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id           UUID NOT NULL,
    client_id             UUID NOT NULL,
    workspace_id          UUID,
    elba_org_id           TEXT,
    elba_contractor_id    TEXT,
    name                  TEXT NOT NULL,
    inn                   TEXT,
    kpp                   TEXT,
    status                TEXT NOT NULL DEFAULT 'active',
    payment_channel       TEXT NOT NULL DEFAULT 'bank',
    notes                 TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_client_payer_name_check
        CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 500),
    CONSTRAINT business_client_payer_status_check
        CHECK (status IN ('active', 'inactive', 'needs_review')),
    CONSTRAINT business_client_payer_channel_check
        CHECK (payment_channel IN ('bank', 'personal_card', 'cash', 'other')),
    CONSTRAINT business_client_payer_identity_check
        CHECK (elba_contractor_id IS NOT NULL OR inn IS NOT NULL OR payment_channel <> 'bank')
);

CREATE TABLE IF NOT EXISTS business_client_project (
    id              UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id     UUID NOT NULL,
    client_id       UUID NOT NULL,
    workspace_id    UUID NOT NULL,
    project_id      UUID NOT NULL,
    service_type    TEXT NOT NULL,
    billable        BOOLEAN NOT NULL DEFAULT true,
    portal_visible  BOOLEAN NOT NULL DEFAULT false,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_client_project_service_check
        CHECK (service_type IN ('development', 'support', 'seo', 'content', 'internal'))
);

CREATE TABLE IF NOT EXISTS business_counterparty_classification (
    id              UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id     UUID NOT NULL,
    workspace_id    UUID,
    source          TEXT NOT NULL,
    external_id     TEXT NOT NULL,
    name            TEXT NOT NULL,
    inn             TEXT,
    classification  TEXT NOT NULL DEFAULT 'unresolved',
    client_id       UUID,
    worker_id       UUID,
    confidence      TEXT NOT NULL DEFAULT 'needs_review',
    reason          TEXT,
    classified_by   UUID,
    classified_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_counterparty_source_check
        CHECK (source IN ('elba', 'bank', 'manual')),
    CONSTRAINT business_counterparty_external_id_check
        CHECK (external_id = btrim(external_id) AND char_length(external_id) BETWEEN 1 AND 500),
    CONSTRAINT business_counterparty_name_check
        CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 500),
    CONSTRAINT business_counterparty_classification_check
        CHECK (classification IN ('client_payer', 'worker_payee', 'vendor', 'transit', 'ignored', 'unresolved')),
    CONSTRAINT business_counterparty_confidence_check
        CHECK (confidence IN ('confirmed', 'needs_review', 'rejected')),
    CONSTRAINT business_counterparty_client_check
        CHECK (classification <> 'client_payer' OR client_id IS NOT NULL),
    CONSTRAINT business_counterparty_worker_check
        CHECK (classification <> 'worker_payee' OR worker_id IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS business_agreement (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id       UUID NOT NULL,
    client_id         UUID NOT NULL,
    project_id        UUID,
    service_type      TEXT NOT NULL,
    agreement_key     TEXT NOT NULL,
    version           INTEGER NOT NULL DEFAULT 1,
    name              TEXT NOT NULL,
    model             TEXT NOT NULL,
    amount_rub        NUMERIC(14,2),
    hourly_rate_rub   NUMERIC(14,2),
    cap_rub           NUMERIC(14,2),
    invoice_day       INTEGER,
    due_days          INTEGER NOT NULL DEFAULT 7,
    period_months     INTEGER NOT NULL DEFAULT 1,
    payment_channel   TEXT NOT NULL DEFAULT 'bank',
    effective_from    DATE NOT NULL,
    effective_to      DATE,
    status            TEXT NOT NULL DEFAULT 'draft',
    is_estimate       BOOLEAN NOT NULL DEFAULT false,
    needs_review      BOOLEAN NOT NULL DEFAULT false,
    terms             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_agreement_service_check
        CHECK (service_type IN ('development', 'support', 'seo', 'content', 'internal')),
    CONSTRAINT business_agreement_key_check
        CHECK (agreement_key = btrim(agreement_key) AND char_length(agreement_key) BETWEEN 1 AND 200),
    CONSTRAINT business_agreement_version_check CHECK (version > 0),
    CONSTRAINT business_agreement_name_check
        CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 300),
    CONSTRAINT business_agreement_model_check
        CHECK (model IN ('fixed', 'cap', 'time_material', 'project')),
    CONSTRAINT business_agreement_amount_check
        CHECK (amount_rub IS NULL OR amount_rub >= 0),
    CONSTRAINT business_agreement_hourly_check
        CHECK (hourly_rate_rub IS NULL OR hourly_rate_rub >= 0),
    CONSTRAINT business_agreement_cap_check
        CHECK (cap_rub IS NULL OR cap_rub >= 0),
    CONSTRAINT business_agreement_invoice_day_check
        CHECK (invoice_day IS NULL OR invoice_day BETWEEN 1 AND 31),
    CONSTRAINT business_agreement_due_days_check CHECK (due_days BETWEEN 0 AND 365),
    CONSTRAINT business_agreement_period_check CHECK (period_months BETWEEN 0 AND 120),
    CONSTRAINT business_agreement_channel_check
        CHECK (payment_channel IN ('bank', 'personal_card', 'cash', 'other')),
    CONSTRAINT business_agreement_dates_check
        CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CONSTRAINT business_agreement_status_check
        CHECK (status IN ('draft', 'active', 'paused', 'expired', 'superseded')),
    CONSTRAINT business_agreement_terms_check CHECK (jsonb_typeof(terms) = 'object')
);

CREATE TABLE IF NOT EXISTS business_receivable (
    id                       UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id              UUID NOT NULL,
    agreement_id             UUID NOT NULL,
    client_id                UUID NOT NULL,
    project_id               UUID,
    period_key               TEXT NOT NULL,
    period_start             DATE NOT NULL,
    period_end               DATE NOT NULL,
    planned_amount_rub       NUMERIC(14,2) NOT NULL,
    paid_amount_rub          NUMERIC(14,2) NOT NULL DEFAULT 0,
    source                   TEXT NOT NULL DEFAULT 'agreement',
    invoice_on               DATE,
    due_on                   DATE,
    status                   TEXT NOT NULL DEFAULT 'expected',
    client_billing_period_id UUID,
    elba_invoice_id          TEXT,
    elba_act_id              TEXT,
    needs_review             BOOLEAN NOT NULL DEFAULT false,
    notes                    TEXT,
    idempotency_key          TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_receivable_period_key_check
        CHECK (period_key = btrim(period_key) AND char_length(period_key) BETWEEN 1 AND 100),
    CONSTRAINT business_receivable_period_check CHECK (period_end >= period_start),
    CONSTRAINT business_receivable_amount_check
        CHECK (planned_amount_rub >= 0 AND paid_amount_rub >= 0),
    CONSTRAINT business_receivable_source_check
        CHECK (source IN ('agreement', 'billing_period', 'manual')),
    CONSTRAINT business_receivable_dates_check
        CHECK (due_on IS NULL OR invoice_on IS NULL OR due_on >= invoice_on),
    CONSTRAINT business_receivable_status_check
        CHECK (status IN ('expected', 'invoiced', 'partially_paid', 'paid', 'overdue', 'skipped', 'written_off')),
    CONSTRAINT business_receivable_idempotency_check
        CHECK (idempotency_key = btrim(idempotency_key) AND char_length(idempotency_key) BETWEEN 1 AND 300)
);

CREATE TABLE IF NOT EXISTS business_bank_import_batch (
    id                 UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id        UUID NOT NULL,
    source             TEXT NOT NULL,
    filename           TEXT NOT NULL,
    file_sha256        TEXT NOT NULL,
    idempotency_key    TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'processing',
    rows_total         INTEGER NOT NULL DEFAULT 0,
    rows_inserted      INTEGER NOT NULL DEFAULT 0,
    rows_duplicate     INTEGER NOT NULL DEFAULT 0,
    rows_invalid       INTEGER NOT NULL DEFAULT 0,
    imported_by        UUID NOT NULL,
    raw_metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message      TEXT,
    completed_at       TIMESTAMPTZ,
    voided_at          TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_bank_import_source_check
        CHECK (source IN ('modulbank_xlsx', 'modulbank_csv', 'modulbank_api')),
    CONSTRAINT business_bank_import_filename_check
        CHECK (filename = btrim(filename) AND char_length(filename) BETWEEN 1 AND 500),
    CONSTRAINT business_bank_import_sha_check CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT business_bank_import_status_check
        CHECK (status IN ('processing', 'completed', 'failed', 'voided')),
    CONSTRAINT business_bank_import_counts_check
        CHECK (rows_total >= 0 AND rows_inserted >= 0 AND rows_duplicate >= 0 AND rows_invalid >= 0),
    CONSTRAINT business_bank_import_metadata_check CHECK (jsonb_typeof(raw_metadata) = 'object')
);

CREATE TABLE IF NOT EXISTS business_bank_transaction (
    id                         UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id                UUID NOT NULL,
    import_batch_id            UUID,
    source                     TEXT NOT NULL,
    external_id                TEXT,
    dedup_key                  TEXT NOT NULL,
    booked_on                  DATE NOT NULL,
    direction                  TEXT NOT NULL,
    amount_rub                 NUMERIC(14,2) NOT NULL,
    currency                   TEXT NOT NULL DEFAULT 'RUB',
    account_mask               TEXT,
    counterparty_name          TEXT NOT NULL,
    counterparty_inn           TEXT,
    counterparty_kpp           TEXT,
    counterparty_account_mask  TEXT,
    purpose                    TEXT,
    classification             TEXT NOT NULL DEFAULT 'unknown',
    classification_confidence  TEXT NOT NULL DEFAULT 'unresolved',
    raw_payload                JSONB NOT NULL DEFAULT '{}'::jsonb,
    voided_at                  TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_bank_transaction_source_check
        CHECK (source IN ('modulbank_api', 'modulbank_xlsx', 'modulbank_csv', 'personal_card', 'manual')),
    CONSTRAINT business_bank_transaction_dedup_check
        CHECK (dedup_key = btrim(dedup_key) AND char_length(dedup_key) BETWEEN 1 AND 300),
    CONSTRAINT business_bank_transaction_direction_check CHECK (direction IN ('inbound', 'outbound')),
    CONSTRAINT business_bank_transaction_amount_check CHECK (amount_rub > 0),
    CONSTRAINT business_bank_transaction_currency_check CHECK (currency = 'RUB'),
    CONSTRAINT business_bank_transaction_counterparty_check
        CHECK (counterparty_name = btrim(counterparty_name) AND char_length(counterparty_name) BETWEEN 1 AND 500),
    CONSTRAINT business_bank_transaction_classification_check
        CHECK (classification IN ('client_income', 'payroll', 'tax', 'service', 'transfer', 'owner_draw', 'vitmax_transit', 'unknown')),
    CONSTRAINT business_bank_transaction_confidence_check
        CHECK (classification_confidence IN ('confirmed', 'suggested', 'unresolved')),
    CONSTRAINT business_bank_transaction_payload_check CHECK (jsonb_typeof(raw_payload) = 'object')
);

CREATE TABLE IF NOT EXISTS business_transaction_match (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id       UUID NOT NULL,
    transaction_id    UUID NOT NULL,
    target_type       TEXT NOT NULL,
    target_id         UUID NOT NULL,
    amount_rub        NUMERIC(14,2) NOT NULL,
    status            TEXT NOT NULL DEFAULT 'suggested',
    suggested_by      UUID,
    confirmed_by      UUID,
    confirmed_at      TIMESTAMPTZ,
    reversed_by       UUID,
    reversed_at       TIMESTAMPTZ,
    idempotency_key   TEXT NOT NULL,
    notes             TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_transaction_match_target_check
        CHECK (target_type IN ('receivable', 'billing_period', 'payout', 'company_cost')),
    CONSTRAINT business_transaction_match_amount_check CHECK (amount_rub > 0),
    CONSTRAINT business_transaction_match_status_check
        CHECK (status IN ('suggested', 'confirmed', 'rejected', 'reversed')),
    CONSTRAINT business_transaction_match_confirmation_check
        CHECK (status <> 'confirmed' OR (confirmed_by IS NOT NULL AND confirmed_at IS NOT NULL)),
    CONSTRAINT business_transaction_match_reversal_check
        CHECK (status <> 'reversed' OR reversed_at IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS business_company_cost (
    id              UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id     UUID NOT NULL,
    transaction_id  UUID,
    category        TEXT NOT NULL,
    amount_rub      NUMERIC(14,2) NOT NULL,
    workspace_id    UUID,
    client_id       UUID,
    project_id      UUID,
    incurred_on     DATE NOT NULL,
    notes           TEXT,
    created_by      UUID NOT NULL,
    voided_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_company_cost_category_check
        CHECK (category IN ('tax', 'bank', 'ai', 'service', 'infrastructure', 'contractor', 'other')),
    CONSTRAINT business_company_cost_amount_check CHECK (amount_rub > 0)
);

CREATE TABLE IF NOT EXISTS business_worker (
    id                     UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id            UUID NOT NULL,
    user_id                UUID,
    name                   TEXT NOT NULL,
    status                 TEXT NOT NULL DEFAULT 'active',
    engagement_format      TEXT NOT NULL,
    recipient_external_id  TEXT,
    recipient_mask         TEXT,
    notes                  TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_worker_name_check
        CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 240),
    CONSTRAINT business_worker_status_check CHECK (status IN ('active', 'paused', 'left')),
    CONSTRAINT business_worker_format_check
        CHECK (engagement_format IN ('employee', 'self_employed', 'individual_contractor', 'vendor_person'))
);

CREATE TABLE IF NOT EXISTS business_compensation_policy (
    id                 UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id        UUID NOT NULL,
    workspace_id       UUID,
    project_id         UUID,
    service_type       TEXT,
    pool               TEXT NOT NULL,
    participant_role   TEXT,
    max_percent        NUMERIC(7,4) NOT NULL,
    default_percent    NUMERIC(7,4),
    effective_from     DATE NOT NULL,
    effective_to       DATE,
    version            INTEGER NOT NULL DEFAULT 1,
    status             TEXT NOT NULL DEFAULT 'active',
    created_by         UUID,
    override_reason    TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_compensation_policy_service_check
        CHECK (service_type IS NULL OR service_type IN ('development', 'support', 'seo', 'content', 'internal')),
    CONSTRAINT business_compensation_policy_pool_check CHECK (pool IN ('pm', 'execution', 'total')),
    CONSTRAINT business_compensation_policy_role_check
        CHECK (participant_role IS NULL OR participant_role IN ('pm', 'executor', 'reviewer', 'seo', 'content', 'copywriter', 'designer', 'domain_reviewer')),
    CONSTRAINT business_compensation_policy_percent_check
        CHECK (max_percent >= 0 AND max_percent <= 100 AND (default_percent IS NULL OR (default_percent >= 0 AND default_percent <= max_percent))),
    CONSTRAINT business_compensation_policy_dates_check
        CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CONSTRAINT business_compensation_policy_version_check CHECK (version > 0),
    CONSTRAINT business_compensation_policy_status_check CHECK (status IN ('active', 'superseded', 'disabled'))
);

CREATE TABLE IF NOT EXISTS business_client_request (
    id                   UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id          UUID NOT NULL,
    client_id            UUID NOT NULL,
    workspace_id         UUID NOT NULL,
    project_id           UUID,
    channel              TEXT NOT NULL,
    external_ref         TEXT,
    idempotency_key      TEXT NOT NULL,
    summary              TEXT NOT NULL,
    received_at          TIMESTAMPTZ NOT NULL,
    triage_due_at        TIMESTAMPTZ,
    triaged_at           TIMESTAMPTZ,
    linked_issue_id      UUID,
    pm_worker_id         UUID,
    status               TEXT NOT NULL DEFAULT 'new',
    client_escalated_at  TIMESTAMPTZ,
    escalation_reason    TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_client_request_channel_check
        CHECK (channel IN ('siteping', 'email', 'messenger', 'meeting', 'manual')),
    CONSTRAINT business_client_request_summary_check
        CHECK (summary = btrim(summary) AND char_length(summary) BETWEEN 1 AND 2000),
    CONSTRAINT business_client_request_status_check
        CHECK (status IN ('new', 'triaged', 'linked', 'closed', 'missed', 'escalated')),
    CONSTRAINT business_client_request_triage_check
        CHECK (triaged_at IS NULL OR triaged_at >= received_at),
    CONSTRAINT business_client_request_escalation_check
        CHECK (status NOT IN ('missed', 'escalated') OR escalation_reason IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS business_task_economics (
    id                         UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id                UUID NOT NULL,
    workspace_id               UUID NOT NULL,
    project_id                 UUID NOT NULL,
    issue_id                   UUID NOT NULL,
    client_id                  UUID,
    client_request_id          UUID,
    version                    INTEGER NOT NULL DEFAULT 1,
    status                     TEXT NOT NULL DEFAULT 'draft',
    service_type               TEXT NOT NULL,
    service_value_rub          NUMERIC(14,2) NOT NULL,
    source                     TEXT NOT NULL,
    billing_disposition        TEXT NOT NULL DEFAULT 'normal',
    client_price_snapshot_rub  NUMERIC(14,2),
    internal_ai_cost_rub       NUMERIC(14,2),
    fx_rate                    NUMERIC(12,4),
    policy_snapshot            JSONB NOT NULL DEFAULT '{}'::jsonb,
    pm_eligible                BOOLEAN NOT NULL DEFAULT true,
    pm_ineligible_reason       TEXT,
    owner_override             BOOLEAN NOT NULL DEFAULT false,
    owner_override_reason      TEXT,
    idempotency_key            TEXT NOT NULL,
    accepted_at                TIMESTAMPTZ,
    accepted_by                UUID,
    superseded_at              TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_task_economics_version_check CHECK (version > 0),
    CONSTRAINT business_task_economics_status_check CHECK (status IN ('draft', 'accepted', 'superseded')),
    CONSTRAINT business_task_economics_service_check
        CHECK (service_type IN ('development', 'support', 'seo', 'content', 'internal')),
    CONSTRAINT business_task_economics_value_check CHECK (service_value_rub >= 0),
    CONSTRAINT business_task_economics_source_check
        CHECK (source IN ('billing_estimate', 'charge_snapshot', 'manual_override')),
    CONSTRAINT business_task_economics_disposition_check
        CHECK (billing_disposition IN ('normal', 'warranty', 'service_recovery', 'non_billable')),
    CONSTRAINT business_task_economics_cost_check
        CHECK ((client_price_snapshot_rub IS NULL OR client_price_snapshot_rub >= 0) AND (internal_ai_cost_rub IS NULL OR internal_ai_cost_rub >= 0)),
    CONSTRAINT business_task_economics_policy_check CHECK (jsonb_typeof(policy_snapshot) = 'object'),
    CONSTRAINT business_task_economics_pm_check CHECK (pm_eligible OR pm_ineligible_reason IS NOT NULL),
    CONSTRAINT business_task_economics_override_check CHECK (owner_override = false OR owner_override_reason IS NOT NULL),
    CONSTRAINT business_task_economics_acceptance_check
        CHECK (status <> 'accepted' OR (accepted_at IS NOT NULL AND accepted_by IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS business_task_participant (
    id                      UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id             UUID NOT NULL,
    task_economics_id       UUID NOT NULL,
    worker_id               UUID NOT NULL,
    role                    TEXT NOT NULL,
    pool                    TEXT NOT NULL,
    weight                  NUMERIC(9,4) NOT NULL DEFAULT 1,
    percent                 NUMERIC(7,4) NOT NULL,
    amount_rub              NUMERIC(14,2) NOT NULL,
    participation_confirmed BOOLEAN NOT NULL DEFAULT false,
    confirmed_by            UUID,
    confirmed_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_task_participant_role_check
        CHECK (role IN ('pm', 'executor', 'reviewer', 'seo', 'content', 'copywriter', 'designer', 'domain_reviewer')),
    CONSTRAINT business_task_participant_pool_check CHECK (pool IN ('pm', 'execution')),
    CONSTRAINT business_task_participant_weight_check CHECK (weight > 0),
    CONSTRAINT business_task_participant_percent_check CHECK (percent >= 0 AND percent <= 100),
    CONSTRAINT business_task_participant_amount_check CHECK (amount_rub >= 0),
    CONSTRAINT business_task_participant_confirmation_check
        CHECK (participation_confirmed = false OR (confirmed_by IS NOT NULL AND confirmed_at IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS business_receivable_task (
    id                    UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id           UUID NOT NULL,
    receivable_id         UUID NOT NULL,
    task_economics_id     UUID NOT NULL,
    service_value_rub     NUMERIC(14,2) NOT NULL,
    allocated_value_rub   NUMERIC(14,2) NOT NULL,
    funded_rub            NUMERIC(14,2) NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_receivable_task_amount_check
        CHECK (service_value_rub >= 0 AND allocated_value_rub > 0 AND allocated_value_rub <= service_value_rub AND funded_rub >= 0)
);

CREATE TABLE IF NOT EXISTS business_accrual (
    id                    UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id           UUID NOT NULL,
    task_economics_id     UUID NOT NULL,
    participant_id        UUID NOT NULL,
    worker_id             UUID NOT NULL,
    role                  TEXT NOT NULL,
    policy_snapshot       JSONB NOT NULL DEFAULT '{}'::jsonb,
    original_amount_rub   NUMERIC(14,2) NOT NULL,
    funded_rub            NUMERIC(14,2) NOT NULL DEFAULT 0,
    reserve_funded_rub    NUMERIC(14,2) NOT NULL DEFAULT 0,
    paid_rub              NUMERIC(14,2) NOT NULL DEFAULT 0,
    holdback_percent      NUMERIC(7,4) NOT NULL DEFAULT 0,
    status                TEXT NOT NULL DEFAULT 'accrued',
    client_funded_at      TIMESTAMPTZ,
    reserve_due_on        DATE,
    payable_at            TIMESTAMPTZ,
    paid_at               TIMESTAMPTZ,
    idempotency_key       TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_accrual_role_check
        CHECK (role IN ('pm', 'executor', 'reviewer', 'seo', 'content', 'copywriter', 'designer', 'domain_reviewer')),
    CONSTRAINT business_accrual_policy_check CHECK (jsonb_typeof(policy_snapshot) = 'object'),
    CONSTRAINT business_accrual_amount_check
        CHECK (original_amount_rub >= 0 AND funded_rub >= 0 AND reserve_funded_rub >= 0 AND paid_rub >= 0),
    CONSTRAINT business_accrual_holdback_check CHECK (holdback_percent >= 0 AND holdback_percent <= 100),
    CONSTRAINT business_accrual_status_check
        CHECK (status IN ('accrued', 'partially_payable', 'payable', 'in_payout', 'paid', 'adjusted'))
);

CREATE TABLE IF NOT EXISTS business_quality_case (
    id                 UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id        UUID NOT NULL,
    issue_id           UUID NOT NULL,
    task_economics_id  UUID,
    status             TEXT NOT NULL DEFAULT 'open',
    severity           TEXT NOT NULL,
    summary            TEXT NOT NULL,
    resolution         TEXT,
    created_by         UUID NOT NULL,
    resolved_by        UUID,
    resolved_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_quality_case_status_check CHECK (status IN ('open', 'resolved', 'dismissed')),
    CONSTRAINT business_quality_case_severity_check CHECK (severity IN ('critical', 'major', 'minor', 'cosmetic')),
    CONSTRAINT business_quality_case_summary_check
        CHECK (summary = btrim(summary) AND char_length(summary) BETWEEN 1 AND 4000),
    CONSTRAINT business_quality_case_resolution_check
        CHECK (status = 'open' OR (resolution IS NOT NULL AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS business_accrual_adjustment (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id       UUID NOT NULL,
    accrual_id        UUID NOT NULL,
    quality_case_id   UUID,
    amount_rub        NUMERIC(14,2) NOT NULL,
    reason            TEXT NOT NULL,
    decision_ref      TEXT,
    notes             TEXT NOT NULL,
    actor_user_id     UUID NOT NULL,
    idempotency_key   TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_accrual_adjustment_amount_check CHECK (amount_rub <> 0),
    CONSTRAINT business_accrual_adjustment_reason_check
        CHECK (reason IN ('quality_case', 'owner_correction', 'migration')),
    CONSTRAINT business_accrual_adjustment_quality_check
        CHECK (reason <> 'quality_case' OR quality_case_id IS NOT NULL),
    CONSTRAINT business_accrual_adjustment_notes_check
        CHECK (notes = btrim(notes) AND char_length(notes) BETWEEN 1 AND 4000)
);

CREATE TABLE IF NOT EXISTS business_reserve_ledger (
    id               UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id      UUID NOT NULL,
    entry_type       TEXT NOT NULL,
    amount_rub       NUMERIC(14,2) NOT NULL,
    accrual_id       UUID,
    payout_batch_id  UUID,
    occurred_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    reason           TEXT NOT NULL,
    actor_user_id    UUID,
    idempotency_key  TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_reserve_entry_type_check
        CHECK (entry_type IN ('contribution', 'allocation', 'release', 'correction')),
    CONSTRAINT business_reserve_amount_check CHECK (amount_rub <> 0),
    CONSTRAINT business_reserve_reason_check
        CHECK (reason = btrim(reason) AND char_length(reason) BETWEEN 1 AND 2000)
);

CREATE TABLE IF NOT EXISTS business_payout_batch (
    id                    UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id           UUID NOT NULL,
    period_key            TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'draft',
    total_rub             NUMERIC(14,2) NOT NULL DEFAULT 0,
    worker_count          INTEGER NOT NULL DEFAULT 0,
    idempotency_key       TEXT NOT NULL,
    approved_by           UUID,
    approved_at           TIMESTAMPTZ,
    submitted_at          TIMESTAMPTZ,
    external_operation_id TEXT,
    paid_at               TIMESTAMPTZ,
    reconciled_at         TIMESTAMPTZ,
    failed_at             TIMESTAMPTZ,
    error_message         TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_payout_batch_period_check
        CHECK (period_key = btrim(period_key) AND char_length(period_key) BETWEEN 1 AND 100),
    CONSTRAINT business_payout_batch_status_check
        CHECK (status IN ('draft', 'approved', 'submitted', 'paid', 'failed', 'reconciled')),
    CONSTRAINT business_payout_batch_amount_check CHECK (total_rub >= 0 AND worker_count >= 0),
    CONSTRAINT business_payout_batch_approval_check
        CHECK (status NOT IN ('approved', 'submitted', 'paid', 'reconciled') OR (approved_by IS NOT NULL AND approved_at IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS business_payout_item (
    id                    UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id           UUID NOT NULL,
    payout_batch_id       UUID NOT NULL,
    accrual_id            UUID NOT NULL,
    worker_id             UUID NOT NULL,
    amount_rub            NUMERIC(14,2) NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending',
    external_operation_id TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_payout_item_amount_check CHECK (amount_rub > 0),
    CONSTRAINT business_payout_item_status_check CHECK (status IN ('pending', 'submitted', 'paid', 'failed', 'void'))
);

CREATE TABLE IF NOT EXISTS business_bank_outbox (
    id                     UUID NOT NULL DEFAULT gen_random_uuid(),
    business_id            UUID NOT NULL,
    aggregate_type         TEXT NOT NULL,
    aggregate_id           UUID NOT NULL,
    event_type             TEXT NOT NULL,
    payload                JSONB NOT NULL,
    status                 TEXT NOT NULL DEFAULT 'pending',
    attempt_count          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    external_operation_id  TEXT,
    idempotency_key        TEXT NOT NULL,
    last_error             TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at                TIMESTAMPTZ,
    CONSTRAINT business_bank_outbox_aggregate_check CHECK (aggregate_type = 'payout_batch'),
    CONSTRAINT business_bank_outbox_event_check CHECK (event_type = 'create_payout_draft'),
    CONSTRAINT business_bank_outbox_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT business_bank_outbox_status_check
        CHECK (status IN ('pending', 'processing', 'sent', 'failed', 'dead_letter')),
    CONSTRAINT business_bank_outbox_attempt_check CHECK (attempt_count >= 0)
);
