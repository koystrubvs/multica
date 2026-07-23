CREATE TABLE business_contract (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id   uuid NOT NULL REFERENCES business_account(id) ON DELETE CASCADE,
    client_id     uuid NOT NULL REFERENCES business_client(id) ON DELETE CASCADE,
    number        text NOT NULL DEFAULT '',
    subject       text NOT NULL DEFAULT '',
    amount_rub    numeric(14,2),
    starts_on     date,
    ends_on       date,
    status        text NOT NULL DEFAULT 'active',
    attachment_id uuid REFERENCES attachment(id) ON DELETE SET NULL,
    notes         text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT business_contract_status_check CHECK (status IN ('draft', 'active', 'expired', 'terminated')),
    CONSTRAINT business_contract_amount_check CHECK (amount_rub IS NULL OR amount_rub >= 0)
);

CREATE INDEX idx_business_contract_client ON business_contract (business_id, client_id, created_at DESC);
