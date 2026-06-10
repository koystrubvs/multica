-- 119: daily USD->RUB FX rates (Bank of Russia / ЦБ РФ). Renumbered from 118 (collision with upstream 118_agent_task_queue…_drop_fk).
--
-- Lets us value historical agent token cost at the exchange rate that was in
-- effect on the day each task ran, instead of re-pricing all of history at a
-- single current rate. Token cost itself is computed in USD on the client
-- (per-model provider list prices); this table supplies the per-date USD->RUB
-- multiplier. Rows are immutable historical facts published by the CBR.
--
-- Populated lazily by GET /api/fx/daily from the CBR daily-dynamics feed
-- (XML_dynamic, currency code R01235 = USD). Only business days are published;
-- callers carry forward the most recent prior rate for weekends/holidays.
CREATE TABLE IF NOT EXISTS fx_rate_daily (
    date       DATE PRIMARY KEY,
    usd_rub    NUMERIC(12,4) NOT NULL,
    source     TEXT NOT NULL DEFAULT 'cbr',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
