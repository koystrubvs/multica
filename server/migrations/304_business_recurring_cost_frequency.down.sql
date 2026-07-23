ALTER TABLE business_recurring_cost
DROP CONSTRAINT IF EXISTS business_recurring_cost_frequency_check;

ALTER TABLE business_recurring_cost
DROP COLUMN IF EXISTS frequency;
