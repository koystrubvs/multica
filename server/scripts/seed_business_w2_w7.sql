\set ON_ERROR_STOP on

-- Initial owner-approved W2-W7 registry for the personal business contour.
-- This seed is idempotent and deliberately creates no historical worker
-- accruals or payouts. Ambiguous payer mappings remain unconfirmed.

BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM business_account ba
        JOIN business_account_member bam
          ON bam.business_id = ba.id
         AND bam.user_id = ba.owner_user_id
         AND bam.role = 'owner'
        JOIN business_workspace bw
          ON bw.business_id = ba.id
         AND bw.workspace_id = '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid
        WHERE ba.id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
          AND ba.owner_user_id = '08eba0b4-4938-4309-9e85-25d191fdd669'::uuid
    ) THEN
        RAISE EXCEPTION 'W1 business/workspace/owner baseline does not match the approved production target';
    END IF;
END $$;

CREATE TEMP TABLE seed_business_client (
    canonical_name TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    payment_channel TEXT NOT NULL,
    notes TEXT,
    archived BOOLEAN NOT NULL DEFAULT false
) ON COMMIT DROP;

INSERT INTO seed_business_client (canonical_name, status, payment_channel, notes, archived) VALUES
    ('Артро', 'active', 'bank', 'artroklinika.ru; личный контур', false),
    ('Бамбино', 'active', 'bank', 'bambinoclinic.ru; личный контур', false),
    ('Center Implant', 'active', 'bank', 'center-implant.ru; payer требует подтверждения', false),
    ('Diagnocat', 'active', 'bank', 'diagnocat.ru; переведён в личный контур', false),
    ('Генетико', 'active', 'bank', 'genetico.ru; личный контур', false),
    ('Инноватис', 'active', 'bank', 'innovatissoft.ru; личный контур', false),
    ('KAN', 'active', 'personal_card', 'kan.uz; оплата лично на карту', false),
    ('Liberty', 'active', 'bank', 'liberty32.ru и libertydeti.ru', false),
    ('Newneuro', 'active', 'bank', 'newneuro.ru; личный контур', false),
    ('Долголетие', 'active', 'bank', 'spina.spb.ru и plastika.me', false),
    ('Сфера', 'active', 'bank', 'sfe.ru; несколько подтверждённых плательщиков', false),
    ('Sugar Media', 'active', 'bank', 'sugarmedia.ru; личный контур', false),
    ('Tinnitus Neuro', 'active', 'bank', 'tinnitusneuro.ru; payer не подтверждён', false),
    ('TrueSmile', 'active', 'bank', 'truesmile.ae; личный контур', false),
    ('Vincea', 'active', 'bank', 'vincea.ru; личный контур', false),
    ('РПК', 'active', 'bank', 'Вне Plane; проектная разработка', false),
    ('Априори Системс', 'active', 'bank', 'Вне Plane; нерегулярная поддержка', false),
    ('РЕМИ', 'active', 'bank', 'Вне Plane; техподдержка WordPress', false),
    ('ТСН На Вайнера 19', 'active', 'bank', 'Вне Plane; техподдержка сайта', false),
    ('БАН', 'active', 'bank', 'Вне Plane; SEO', false),
    ('МЦ Европа', 'lost', 'bank', 'reconstruction.evropa-clinic.ru; работ нет с декабря 2025', true),
    ('Rooblook', 'lost', 'bank', 'rooblook.ru; работ нет, долг закрыт', true);

UPDATE business_client c
SET status = s.status,
    primary_payment_channel = s.payment_channel,
    notes = s.notes,
    archived_at = CASE WHEN s.archived THEN COALESCE(c.archived_at, now()) ELSE NULL END,
    updated_at = now()
FROM seed_business_client s
WHERE c.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
  AND c.canonical_name = s.canonical_name;

INSERT INTO business_client (
    business_id, canonical_name, status, primary_payment_channel, notes, archived_at
)
SELECT
    '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid,
    s.canonical_name,
    s.status,
    s.payment_channel,
    s.notes,
    CASE WHEN s.archived THEN now() ELSE NULL END
FROM seed_business_client s
WHERE NOT EXISTS (
    SELECT 1 FROM business_client c
    WHERE c.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
      AND c.canonical_name = s.canonical_name
);

CREATE TEMP TABLE seed_business_alias (
    canonical_name TEXT NOT NULL,
    alias_type TEXT NOT NULL,
    value TEXT NOT NULL,
    normalized_value TEXT NOT NULL
) ON COMMIT DROP;

INSERT INTO seed_business_alias (canonical_name, alias_type, value, normalized_value) VALUES
    ('Артро', 'domain', 'artroklinika.ru', 'artroklinika.ru'),
    ('Артро', 'inn', '3849037180', '3849037180'),
    ('Бамбино', 'domain', 'bambinoclinic.ru', 'bambinoclinic.ru'),
    ('Бамбино', 'inn', '5017130945', '5017130945'),
    ('Center Implant', 'domain', 'center-implant.ru', 'center-implant.ru'),
    ('Diagnocat', 'domain', 'diagnocat.ru', 'diagnocat.ru'),
    ('Генетико', 'domain', 'genetico.ru', 'genetico.ru'),
    ('Генетико', 'inn', '9731078633', '9731078633'),
    ('Инноватис', 'domain', 'innovatissoft.ru', 'innovatissoft.ru'),
    ('Инноватис', 'domain', 'invts.ru', 'invts.ru'),
    ('Инноватис', 'inn', '7451343518', '7451343518'),
    ('KAN', 'domain', 'kan.uz', 'kan.uz'),
    ('Liberty', 'domain', 'liberty32.ru', 'liberty32.ru'),
    ('Liberty', 'domain', 'libertydeti.ru', 'libertydeti.ru'),
    ('Liberty', 'inn', '5027249019', '5027249019'),
    ('Newneuro', 'domain', 'newneuro.ru', 'newneuro.ru'),
    ('Newneuro', 'inn', '7734440833', '7734440833'),
    ('Долголетие', 'domain', 'spina.spb.ru', 'spina.spb.ru'),
    ('Долголетие', 'domain', 'plastika.me', 'plastika.me'),
    ('Долголетие', 'inn', '7813670692', '7813670692'),
    ('Сфера', 'domain', 'sfe.ru', 'sfe.ru'),
    ('Сфера', 'inn', '7714223904', '7714223904'),
    ('Сфера', 'inn', '7727528928', '7727528928'),
    ('Сфера', 'inn', '7727484163', '7727484163'),
    ('Sugar Media', 'domain', 'sugarmedia.ru', 'sugarmedia.ru'),
    ('Sugar Media', 'inn', '6658532076', '6658532076'),
    ('Tinnitus Neuro', 'domain', 'tinnitusneuro.ru', 'tinnitusneuro.ru'),
    ('TrueSmile', 'domain', 'truesmile.ae', 'truesmile.ae'),
    ('TrueSmile', 'inn', '502010332235', '502010332235'),
    ('Vincea', 'domain', 'vincea.ru', 'vincea.ru'),
    ('Vincea', 'inn', '7734429004', '7734429004'),
    ('РПК', 'inn', '4009009703', '4009009703'),
    ('Априори Системс', 'inn', '7811435083', '7811435083'),
    ('РЕМИ', 'inn', '7806419569', '7806419569'),
    ('ТСН На Вайнера 19', 'domain', 'тцбум.рф', 'тцбум.рф'),
    ('ТСН На Вайнера 19', 'inn', '6671161670', '6671161670'),
    ('БАН', 'inn', '7724399880', '7724399880'),
    ('МЦ Европа', 'domain', 'reconstruction.evropa-clinic.ru', 'reconstruction.evropa-clinic.ru'),
    ('МЦ Европа', 'inn', '3702684452', '3702684452'),
    ('Rooblook', 'domain', 'rooblook.ru', 'rooblook.ru'),
    ('Rooblook', 'inn', '701710694481', '701710694481');

INSERT INTO business_client_alias (
    business_id, client_id, source, alias_type, value, normalized_value, confidence, auto_match
)
SELECT
    c.business_id, c.id, 'manual', s.alias_type, s.value, s.normalized_value,
    'confirmed', true
FROM seed_business_alias s
JOIN business_client c
  ON c.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
 AND c.canonical_name = s.canonical_name
ON CONFLICT (business_id, source, alias_type, normalized_value)
DO UPDATE SET
    client_id = EXCLUDED.client_id,
    value = EXCLUDED.value,
    confidence = EXCLUDED.confidence,
    auto_match = EXCLUDED.auto_match,
    updated_at = now();

CREATE TEMP TABLE seed_business_payer (
    canonical_name TEXT NOT NULL,
    name TEXT NOT NULL,
    inn TEXT,
    elba_contractor_id TEXT,
    payment_channel TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    notes TEXT
) ON COMMIT DROP;

INSERT INTO seed_business_payer (
    canonical_name, name, inn, elba_contractor_id, payment_channel, status, notes
) VALUES
    ('Артро', 'ООО «Ортофарма»', '3849037180', '64add585-7b6d-4479-83cf-ae33540c40bf', 'bank', 'active', NULL),
    ('Бамбино', 'ООО «Здоровое Поколение»', '5017130945', '25d22fae-1e41-4dd1-9709-4fd480eba62a', 'bank', 'active', NULL),
    ('Генетико', 'ПАО «ЦГРМ Генетико»', '9731078633', '8e175437-4312-47c7-b795-762fe39f6684', 'bank', 'active', 'В источнике указаны два КПП'),
    ('Инноватис', 'ООО «Инноватис»', '7451343518', 'eb97063d-5499-4d2f-993a-5b5838306958', 'bank', 'active', NULL),
    ('KAN', 'KAN — личная оплата', NULL, NULL, 'personal_card', 'active', 'Платит лично на карту; юрлицо не подтверждено'),
    ('Liberty', 'ООО «Либерти»', '5027249019', '798e08f8-44f4-4e2a-a2b6-d0ee42bea28b', 'bank', 'active', 'Один плательщик для двух проектов'),
    ('Newneuro', 'ООО «Клиника восстановительной неврологии»', '7734440833', 'c6e6cd8e-424a-41bc-95fb-9c3c23a23be4', 'bank', 'active', NULL),
    ('Долголетие', 'ООО «Долголетие»', '7813670692', 'a33a1395-4507-4428-8bf0-468eae8d2e11', 'bank', 'active', 'Плательщик для spina.spb.ru; plastika.me без отдельного Elba mapping'),
    ('Сфера', 'ООО «Ангрис»', '7714223904', '3e202393-1f81-49da-b69d-4fae867488f5', 'bank', 'active', NULL),
    ('Сфера', 'ООО «Клиника Сфера»', '7727528928', NULL, 'bank', 'active', NULL),
    ('Сфера', 'ООО ЦМ «Сфера»', '7727484163', NULL, 'bank', 'active', NULL),
    ('Sugar Media', 'ООО «Сахар Медиа»', '6658532076', '39179c56-77f0-4543-9468-85fb63251759', 'bank', 'active', 'В Эльбе также встречается как ООО СМ'),
    ('TrueSmile', 'ИП Ефремова М.В.', '502010332235', 'f93cc279-52bf-43b5-92ea-a87f0fcaca30', 'bank', 'active', NULL),
    ('Vincea', 'ООО «Фортуна»', '7734429004', 'bfd1ce9b-980c-46c6-a4f4-a630c15f0c23', 'bank', 'active', NULL),
    ('РПК', 'ООО «РПК»', '4009009703', NULL, 'bank', 'active', NULL),
    ('Априори Системс', 'ООО «Априори Системс»', '7811435083', NULL, 'bank', 'active', NULL),
    ('РЕМИ', 'ОП ООО «РЕМИ»', '7806419569', NULL, 'bank', 'active', NULL),
    ('ТСН На Вайнера 19', 'ТСН «На Вайнера 19»', '6671161670', NULL, 'bank', 'active', NULL),
    ('БАН', 'ООО «БАН»', '7724399880', NULL, 'bank', 'active', NULL),
    ('МЦ Европа', 'ООО «МЦ Европа»', '3702684452', '9dd869b6-0e7e-4d18-8e0e-2d1c00219064', 'bank', 'inactive', NULL),
    ('Rooblook', 'ИП Луковенко А.А.', '701710694481', NULL, 'bank', 'inactive', 'Фактический плательщик закрывающего долг платежа');

UPDATE business_client_payer p
SET client_id = c.id,
    workspace_id = '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid,
    elba_contractor_id = s.elba_contractor_id,
    name = s.name,
    inn = s.inn,
    status = s.status,
    payment_channel = s.payment_channel,
    notes = s.notes,
    updated_at = now()
FROM seed_business_payer s
JOIN business_client c
  ON c.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
 AND c.canonical_name = s.canonical_name
WHERE p.business_id = c.business_id
  AND (
      (s.elba_contractor_id IS NOT NULL AND p.elba_contractor_id = s.elba_contractor_id)
      OR (s.inn IS NOT NULL AND p.inn = s.inn)
      OR (s.inn IS NULL AND s.elba_contractor_id IS NULL AND p.name = s.name)
  );

INSERT INTO business_client_payer (
    business_id, client_id, workspace_id, elba_contractor_id, name, inn,
    status, payment_channel, notes
)
SELECT
    c.business_id, c.id, '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid,
    s.elba_contractor_id, s.name, s.inn, s.status, s.payment_channel, s.notes
FROM seed_business_payer s
JOIN business_client c
  ON c.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
 AND c.canonical_name = s.canonical_name
WHERE NOT EXISTS (
    SELECT 1 FROM business_client_payer p
    WHERE p.business_id = c.business_id
      AND (
          (s.elba_contractor_id IS NOT NULL AND p.elba_contractor_id = s.elba_contractor_id)
          OR (s.inn IS NOT NULL AND p.inn = s.inn)
          OR (s.inn IS NULL AND s.elba_contractor_id IS NULL AND p.name = s.name)
      )
);

CREATE TEMP TABLE seed_business_project (
    project_title TEXT PRIMARY KEY,
    canonical_name TEXT NOT NULL,
    service_type TEXT NOT NULL,
    billable BOOLEAN NOT NULL,
    notes TEXT
) ON COMMIT DROP;

INSERT INTO seed_business_project (project_title, canonical_name, service_type, billable, notes) VALUES
    ('artroklinika.ru', 'Артро', 'support', true, NULL),
    ('bambinoclinic.ru', 'Бамбино', 'support', true, NULL),
    ('center-implant.ru', 'Center Implant', 'development', true, 'Payer требует подтверждения'),
    ('diagnocat.ru', 'Diagnocat', 'seo', true, 'SEO + техподдержка'),
    ('genetico.ru', 'Генетико', 'support', true, NULL),
    ('innovatissoft.ru', 'Инноватис', 'support', true, NULL),
    ('kan.uz', 'KAN', 'support', true, 'Оплата на личную карту'),
    ('liberty32.ru', 'Liberty', 'support', true, NULL),
    ('libertydeti.ru', 'Liberty', 'support', true, NULL),
    ('newneuro.ru', 'Newneuro', 'support', true, NULL),
    ('spina.spb.ru', 'Долголетие', 'support', true, NULL),
    ('plastika.me', 'Долголетие', 'seo', true, 'SEO-договорённость 50 000 ₽/мес'),
    ('sfe.ru', 'Сфера', 'support', true, NULL),
    ('sugarmedia.ru', 'Sugar Media', 'support', true, NULL),
    ('tinnitusneuro.ru', 'Tinnitus Neuro', 'support', true, 'Payer требует подтверждения'),
    ('truesmile.ae', 'TrueSmile', 'support', true, 'Также действует отдельное SEO-соглашение'),
    ('vincea.ru', 'Vincea', 'support', true, NULL),
    ('reconstruction.evropa-clinic.ru', 'МЦ Европа', 'support', false, 'Архивный клиент'),
    ('rooblook.ru', 'Rooblook', 'support', false, 'Архивный клиент');

INSERT INTO business_client_project (
    business_id, client_id, workspace_id, project_id, service_type, billable, portal_visible, notes
)
SELECT
    c.business_id, c.id, p.workspace_id, p.id, s.service_type, s.billable, false, s.notes
FROM seed_business_project s
JOIN project p
  ON p.workspace_id = '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid
 AND p.title = s.project_title
JOIN business_client c
  ON c.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
 AND c.canonical_name = s.canonical_name
ON CONFLICT (project_id)
DO UPDATE SET
    business_id = EXCLUDED.business_id,
    client_id = EXCLUDED.client_id,
    workspace_id = EXCLUDED.workspace_id,
    service_type = EXCLUDED.service_type,
    billable = EXCLUDED.billable,
    portal_visible = EXCLUDED.portal_visible,
    notes = EXCLUDED.notes,
    updated_at = now();

INSERT INTO business_counterparty_classification (
    business_id, workspace_id, source, external_id, name, inn, classification,
    client_id, confidence, reason, classified_by, classified_at
)
SELECT
    p.business_id,
    p.workspace_id,
    'bank',
    'inn:' || p.inn,
    p.name,
    p.inn,
    'client_payer',
    p.client_id,
    'confirmed',
    'Owner-approved personal business payer registry',
    '08eba0b4-4938-4309-9e85-25d191fdd669'::uuid,
    now()
FROM business_client_payer p
WHERE p.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
  AND p.inn IS NOT NULL
  AND p.status = 'active'
ON CONFLICT (business_id, source, external_id)
DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    name = EXCLUDED.name,
    inn = EXCLUDED.inn,
    classification = EXCLUDED.classification,
    client_id = EXCLUDED.client_id,
    worker_id = NULL,
    confidence = EXCLUDED.confidence,
    reason = EXCLUDED.reason,
    classified_by = EXCLUDED.classified_by,
    classified_at = EXCLUDED.classified_at,
    updated_at = now();

INSERT INTO business_counterparty_classification (
    business_id, workspace_id, source, external_id, name, inn, classification,
    confidence, reason, classified_by, classified_at
) VALUES
    ('6a833fce-02dd-418f-94b9-3061514e7f20', '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e', 'bank', 'inn:7724010662', 'ФГБУЗ КБ №85 ФМБА', '7724010662', 'transit', 'confirmed', 'VitMax transit; excluded from personal revenue', '08eba0b4-4938-4309-9e85-25d191fdd669', now()),
    ('6a833fce-02dd-418f-94b9-3061514e7f20', '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e', 'bank', 'inn:5611054231', 'ООО «Линия Здоровья»', '5611054231', 'transit', 'confirmed', 'VitMax transit; excluded from personal revenue', '08eba0b4-4938-4309-9e85-25d191fdd669', now()),
    ('6a833fce-02dd-418f-94b9-3061514e7f20', '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e', 'bank', 'inn:9203005794', 'ООО «СК Витязь»', '9203005794', 'transit', 'confirmed', 'VitMax transit; excluded from personal revenue', '08eba0b4-4938-4309-9e85-25d191fdd669', now()),
    ('6a833fce-02dd-418f-94b9-3061514e7f20', '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e', 'bank', 'inn:6674136513', 'ООО «Студия-С»', '6674136513', 'transit', 'confirmed', 'VitMax transit; excluded from personal revenue', '08eba0b4-4938-4309-9e85-25d191fdd669', now()),
    ('6a833fce-02dd-418f-94b9-3061514e7f20', '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e', 'bank', 'inn:662607078800', 'ИП Койструб', '662607078800', 'ignored', 'confirmed', 'Own-account transfer; excluded from revenue and expenses', '08eba0b4-4938-4309-9e85-25d191fdd669', now())
ON CONFLICT (business_id, source, external_id)
DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    name = EXCLUDED.name,
    inn = EXCLUDED.inn,
    classification = EXCLUDED.classification,
    client_id = NULL,
    worker_id = NULL,
    confidence = EXCLUDED.confidence,
    reason = EXCLUDED.reason,
    classified_by = EXCLUDED.classified_by,
    classified_at = EXCLUDED.classified_at,
    updated_at = now();

CREATE TEMP TABLE seed_business_agreement (
    canonical_name TEXT NOT NULL,
    project_title TEXT,
    service_type TEXT NOT NULL,
    agreement_key TEXT NOT NULL,
    name TEXT NOT NULL,
    model TEXT NOT NULL,
    amount_rub NUMERIC(14,2),
    hourly_rate_rub NUMERIC(14,2),
    cap_rub NUMERIC(14,2),
    invoice_day INTEGER,
    due_days INTEGER NOT NULL,
    period_months INTEGER NOT NULL,
    payment_channel TEXT NOT NULL,
    effective_from DATE NOT NULL,
    needs_review BOOLEAN NOT NULL,
    terms JSONB NOT NULL
) ON COMMIT DROP;

INSERT INTO seed_business_agreement VALUES
    ('Артро', 'artroklinika.ru', 'support', 'artro-support', 'Артро — техподдержка', 'cap', NULL, 2500, 25000, 30, 7, 1, 'bank', '2026-07-01', false, '{"cap_kind":"owner_guideline","note":"Превышение планки является рабочей нормой и оформляется отдельно"}'),
    ('Бамбино', 'bambinoclinic.ru', 'support', 'bambino-support', 'Бамбино — техподдержка по факту', 'time_material', NULL, 2500, NULL, 30, 7, 1, 'bank', '2026-07-01', false, '{}'),
    ('Center Implant', 'center-implant.ru', 'development', 'center-implant-site', 'Center Implant — разработка сайта', 'project', 110000, NULL, NULL, NULL, 7, 0, 'bank', '2026-07-01', false, '{"already_paid":true,"payer_needs_review":true}'),
    ('Diagnocat', 'diagnocat.ru', 'seo', 'diagnocat-seo-support', 'Diagnocat — SEO + техподдержка', 'fixed', 150000, NULL, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"invoice_day_needs_confirmation":true}'),
    ('Генетико', 'genetico.ru', 'support', 'genetico-support', 'Генетико — техподдержка', 'cap', NULL, 2000, 25000, 30, 7, 1, 'bank', '2026-07-01', false, '{"cap_kind":"owner_guideline","note":"Превышение планки может оформляться отдельно"}'),
    ('Инноватис', 'innovatissoft.ru', 'support', 'innovatis-support', 'Инноватис — техподдержка по факту', 'time_material', NULL, 2500, NULL, 30, 7, 1, 'bank', '2026-07-01', false, '{"cap_needs_confirmation":true}'),
    ('KAN', 'kan.uz', 'support', 'kan-support', 'KAN — техподдержка', 'fixed', 25000, NULL, NULL, 30, 0, 1, 'personal_card', '2026-07-01', false, '{}'),
    ('Liberty', 'liberty32.ru', 'support', 'liberty32-support', 'Liberty — взрослый сайт, по факту', 'time_material', NULL, 2500, NULL, 30, 7, 1, 'bank', '2026-07-01', false, '{}'),
    ('Liberty', 'libertydeti.ru', 'support', 'libertydeti-support', 'Liberty — детский сайт, по факту', 'time_material', NULL, 2500, NULL, 30, 7, 1, 'bank', '2026-07-01', false, '{}'),
    ('Newneuro', 'newneuro.ru', 'support', 'newneuro-support', 'Newneuro — техподдержка по факту', 'time_material', NULL, 2000, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"invoice_day_needs_confirmation":true}'),
    ('Долголетие', NULL, 'support', 'dolgoletie-support', 'Долголетие — spina + plastika, техподдержка', 'cap', NULL, 2500, 100000, 17, 7, 1, 'bank', '2026-07-01', false, '{"projects":["spina.spb.ru","plastika.me"],"cap_kind":"owner_guideline"}'),
    ('Долголетие', 'plastika.me', 'seo', 'plastika-seo', 'Пластика — SEO', 'fixed', 50000, NULL, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"invoice_day_needs_confirmation":true}'),
    ('Сфера', 'sfe.ru', 'support', 'sfera-support', 'Сфера — техподдержка по факту', 'time_material', NULL, 2500, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"invoice_day_needs_confirmation":true}'),
    ('Sugar Media', 'sugarmedia.ru', 'support', 'sugar-support', 'Sugar Media — техподдержка', 'cap', NULL, 2500, 70000, 30, 7, 1, 'bank', '2026-07-01', false, '{"cap_kind":"owner_guideline"}'),
    ('Tinnitus Neuro', 'tinnitusneuro.ru', 'support', 'tinnitus-support', 'Tinnitus Neuro — техподдержка по факту', 'time_material', NULL, 2500, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"payer_and_invoice_day_need_confirmation":true}'),
    ('TrueSmile', 'truesmile.ae', 'support', 'truesmile-support', 'TrueSmile — техподдержка', 'cap', NULL, 2500, 25000, 30, 7, 1, 'bank', '2026-07-01', false, '{"cap_kind":"owner_guideline"}'),
    ('TrueSmile', 'truesmile.ae', 'seo', 'truesmile-seo', 'TrueSmile — SEO', 'fixed', 50000, NULL, NULL, 8, 7, 1, 'bank', '2026-07-01', false, '{}'),
    ('Vincea', 'vincea.ru', 'support', 'vincea-support', 'Vincea — техподдержка по факту', 'time_material', NULL, 2500, NULL, 1, 7, 1, 'bank', '2026-07-01', false, '{}'),
    ('РПК', NULL, 'development', 'rpk-site', 'РПК — разработка сайта по этапам', 'project', 1100000, NULL, NULL, NULL, 7, 0, 'bank', '2026-01-01', false, '{"paid_before_cutover_rub":697000,"remaining_estimate_rub":403000}'),
    ('Априори Системс', NULL, 'support', 'apriori-support', 'Априори Системс — нерегулярная поддержка', 'time_material', NULL, NULL, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"rate_and_invoice_day_need_confirmation":true}'),
    ('РЕМИ', NULL, 'support', 'remi-support', 'РЕМИ — техподдержка WordPress', 'fixed', 20000, NULL, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"invoice_day_needs_confirmation":true}'),
    ('ТСН На Вайнера 19', NULL, 'support', 'vaynera-support', 'ТСН На Вайнера 19 — техподдержка', 'fixed', 13000, NULL, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"invoice_day_needs_confirmation":true}'),
    ('БАН', NULL, 'seo', 'ban-seo', 'БАН — SEO', 'fixed', 10000, NULL, NULL, NULL, 7, 1, 'bank', '2026-07-01', true, '{"invoice_day_needs_confirmation":true}');

INSERT INTO business_agreement (
    business_id, client_id, project_id, service_type, agreement_key, version, name, model,
    amount_rub, hourly_rate_rub, cap_rub, invoice_day, due_days, period_months,
    payment_channel, effective_from, status, is_estimate, needs_review, terms, created_by
)
SELECT
    c.business_id, c.id, p.id, s.service_type, s.agreement_key, 1, s.name, s.model,
    s.amount_rub, s.hourly_rate_rub, s.cap_rub, s.invoice_day, s.due_days, s.period_months,
    s.payment_channel, s.effective_from, 'active', s.model IN ('cap','time_material'),
    s.needs_review, s.terms, '08eba0b4-4938-4309-9e85-25d191fdd669'::uuid
FROM seed_business_agreement s
JOIN business_client c
  ON c.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
 AND c.canonical_name = s.canonical_name
LEFT JOIN project p
  ON p.workspace_id = '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid
 AND p.title = s.project_title
ON CONFLICT (business_id, agreement_key, version)
DO UPDATE SET
    client_id = EXCLUDED.client_id,
    project_id = EXCLUDED.project_id,
    service_type = EXCLUDED.service_type,
    name = EXCLUDED.name,
    model = EXCLUDED.model,
    amount_rub = EXCLUDED.amount_rub,
    hourly_rate_rub = EXCLUDED.hourly_rate_rub,
    cap_rub = EXCLUDED.cap_rub,
    invoice_day = EXCLUDED.invoice_day,
    due_days = EXCLUDED.due_days,
    period_months = EXCLUDED.period_months,
    payment_channel = EXCLUDED.payment_channel,
    effective_from = EXCLUDED.effective_from,
    status = EXCLUDED.status,
    is_estimate = EXCLUDED.is_estimate,
    needs_review = EXCLUDED.needs_review,
    terms = EXCLUDED.terms,
    updated_at = now();

CREATE TEMP TABLE seed_business_worker (
    user_id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    engagement_format TEXT NOT NULL,
    notes TEXT
) ON COMMIT DROP;

INSERT INTO seed_business_worker VALUES
    ('c49b4965-a119-4c49-a498-1583d73b6a37', 'Александра Катари', 'self_employed', 'ПМ; лимит роли задаётся политикой задачи'),
    ('7a6b786f-5cee-46ae-9131-111359675b40', 'Саша — программист', 'self_employed', 'Исполнитель и профессиональная приёмка'),
    ('e06a7b96-2c68-483e-9422-043a55b2d7ff', 'Татьяна', 'self_employed', 'Участник команды; роль назначается на задаче');

INSERT INTO business_worker (business_id, user_id, name, status, engagement_format, notes)
SELECT
    '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid,
    s.user_id, s.name, 'active', s.engagement_format, s.notes
FROM seed_business_worker s
ON CONFLICT (business_id, user_id) WHERE user_id IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    status = EXCLUDED.status,
    engagement_format = EXCLUDED.engagement_format,
    notes = EXCLUDED.notes,
    updated_at = now();

CREATE TEMP TABLE seed_business_policy (
    service_type TEXT NOT NULL,
    pool TEXT NOT NULL,
    participant_role TEXT,
    max_percent NUMERIC(7,4) NOT NULL,
    default_percent NUMERIC(7,4),
    PRIMARY KEY (service_type, pool)
) ON COMMIT DROP;

INSERT INTO seed_business_policy VALUES
    ('development', 'pm', 'pm', 10, 10),
    ('development', 'execution', NULL, 25, 25),
    ('development', 'total', NULL, 35, 35),
    ('support', 'pm', 'pm', 10, 10),
    ('support', 'execution', NULL, 25, 25),
    ('support', 'total', NULL, 35, 35),
    ('seo', 'pm', 'pm', 10, 10),
    ('seo', 'execution', NULL, 35, 25),
    ('seo', 'total', NULL, 45, 35),
    ('content', 'pm', 'pm', 10, 10),
    ('content', 'execution', NULL, 35, 25),
    ('content', 'total', NULL, 45, 35);

UPDATE business_compensation_policy p
SET participant_role = s.participant_role,
    max_percent = s.max_percent,
    default_percent = s.default_percent,
    status = 'active',
    override_reason = 'Owner-approved W0 policy; initial holdback is zero',
    updated_at = now()
FROM seed_business_policy s
WHERE p.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
  AND p.workspace_id = '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid
  AND p.project_id IS NULL
  AND p.service_type = s.service_type
  AND p.pool = s.pool
  AND p.effective_from = '2026-07-19'::date
  AND p.version = 1;

INSERT INTO business_compensation_policy (
    business_id, workspace_id, service_type, pool, participant_role,
    max_percent, default_percent, effective_from, version, status,
    created_by, override_reason
)
SELECT
    '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid,
    '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid,
    s.service_type, s.pool, s.participant_role, s.max_percent, s.default_percent,
    '2026-07-19'::date, 1, 'active',
    '08eba0b4-4938-4309-9e85-25d191fdd669'::uuid,
    'Owner-approved W0 policy; initial holdback is zero'
FROM seed_business_policy s
WHERE NOT EXISTS (
    SELECT 1 FROM business_compensation_policy p
    WHERE p.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
      AND p.workspace_id = '55c8fded-7f7d-4f9a-8fcc-0e4aa007e96e'::uuid
      AND p.project_id IS NULL
      AND p.service_type = s.service_type
      AND p.pool = s.pool
      AND p.effective_from = '2026-07-19'::date
      AND p.version = 1
);

-- Create the current three-month calendar. T&M rows use existing native
-- billing periods when present; otherwise they remain zero and needs_review.
WITH periods AS (
    SELECT gs::date AS period_start,
           (gs + interval '1 month - 1 day')::date AS period_end,
           to_char(gs, 'YYYY-MM') AS period_key
    FROM generate_series('2026-07-01'::date, '2026-09-01'::date, interval '1 month') gs
), candidates AS (
    SELECT
        a.*,
        p.period_start,
        p.period_end,
        p.period_key,
        bp.id AS billing_period_id,
        bp.total_rub AS billing_period_total,
        CASE
            WHEN a.invoice_day IS NULL THEN NULL
            ELSE make_date(
                extract(year FROM p.period_start)::integer,
                extract(month FROM p.period_start)::integer,
                LEAST(a.invoice_day, extract(day FROM p.period_end)::integer)
            )
        END AS invoice_on
    FROM business_agreement a
    CROSS JOIN periods p
    LEFT JOIN LATERAL (
        SELECT cbp.id, cbp.total_rub
        FROM client_billing_period cbp
        WHERE cbp.project_id = a.project_id
          AND cbp.starts_on < p.period_end + 1
          AND cbp.ends_on > p.period_start
        ORDER BY cbp.starts_on DESC
        LIMIT 1
    ) bp ON a.model = 'time_material'
    WHERE a.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
      AND a.status = 'active'
      AND a.period_months > 0
      AND a.effective_from <= p.period_end
      AND (a.effective_to IS NULL OR a.effective_to >= p.period_start)
)
INSERT INTO business_receivable (
    business_id, agreement_id, client_id, project_id, period_key,
    period_start, period_end, planned_amount_rub, source, invoice_on, due_on,
    client_billing_period_id, needs_review, idempotency_key
)
SELECT
    c.business_id,
    c.id,
    c.client_id,
    c.project_id,
    c.period_key,
    c.period_start,
    c.period_end,
    CASE c.model
        WHEN 'fixed' THEN COALESCE(c.amount_rub, 0)
        WHEN 'cap' THEN COALESCE(c.cap_rub, 0)
        WHEN 'time_material' THEN COALESCE(c.billing_period_total, 0)
        ELSE COALESCE(c.amount_rub, 0)
    END,
    CASE WHEN c.model = 'time_material' AND c.billing_period_id IS NOT NULL
         THEN 'billing_period' ELSE 'agreement' END,
    c.invoice_on,
    CASE WHEN c.invoice_on IS NULL THEN NULL ELSE c.invoice_on + c.due_days END,
    c.billing_period_id,
    c.needs_review OR (c.model = 'time_material' AND c.billing_period_id IS NULL),
    c.id::text || ':' || c.period_key
FROM candidates c
ON CONFLICT (agreement_id, period_key) DO NOTHING;

-- The RPK open project balance is current operational data, not a historical
-- employee accrual. It stays needs_review because the source labels 403k as an estimate.
INSERT INTO business_receivable (
    business_id, agreement_id, client_id, period_key, period_start, period_end,
    planned_amount_rub, source, status, needs_review, notes, idempotency_key
)
SELECT
    a.business_id, a.id, a.client_id, 'project-balance', '2026-07-01', '2026-07-31',
    403000, 'manual', 'expected', true,
    'Остаток по контракту 1 100 000 ₽ после 697 000 ₽, оплаченных до cutover; подтвердить график этапов',
    a.id::text || ':project-balance'
FROM business_agreement a
WHERE a.business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
  AND a.agreement_key = 'rpk-site'
  AND a.version = 1
ON CONFLICT (agreement_id, period_key) DO NOTHING;

INSERT INTO business_audit_event (
    business_id, actor_user_id, actor_type, action, entity_type, request_id, reason, after_data
)
SELECT
    '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid,
    '08eba0b4-4938-4309-9e85-25d191fdd669'::uuid,
    'migration',
    'business.initial_registry.seeded',
    'business_account',
    'seed:w2w7:2026-07-19',
    'Owner-approved W2-W7 initial personal business registry',
    jsonb_build_object(
        'historical_accruals_created', false,
        'vitmax_included_as_client', false,
        'calendar_from', '2026-07',
        'calendar_through', '2026-09'
    )
WHERE NOT EXISTS (
    SELECT 1 FROM business_audit_event
    WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
      AND request_id = 'seed:w2w7:2026-07-19'
);

COMMIT;

SELECT 'business_client' AS entity, count(*) AS rows
FROM business_client
WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
UNION ALL
SELECT 'business_client_payer', count(*)
FROM business_client_payer
WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
UNION ALL
SELECT 'business_client_project', count(*)
FROM business_client_project
WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
UNION ALL
SELECT 'business_agreement', count(*)
FROM business_agreement
WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
UNION ALL
SELECT 'business_receivable', count(*)
FROM business_receivable
WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
UNION ALL
SELECT 'business_worker', count(*)
FROM business_worker
WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
UNION ALL
SELECT 'business_compensation_policy', count(*)
FROM business_compensation_policy
WHERE business_id = '6a833fce-02dd-418f-94b9-3061514e7f20'::uuid
ORDER BY entity;
