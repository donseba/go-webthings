-- Settings per subject (usually an event) that visitors can register for.
-- Prices are in cents. Without closes_at, registration for an event closes
-- when it starts. What registrants read (notes, payment link) is per
-- language in wt_registration_subject_text.
CREATE TABLE wt_registration_subject (
    instance       VARCHAR(64) NOT NULL,
    subject_id     VARCHAR(64) NOT NULL,
    title          VARCHAR(200) NOT NULL DEFAULT '',
    is_open        BOOLEAN NOT NULL DEFAULT FALSE,
    capacity       INTEGER NOT NULL DEFAULT 0,
    closes_at      TIMESTAMPTZ,
    adult_price    BIGINT NOT NULL DEFAULT 0,
    child_price    BIGINT NOT NULL DEFAULT 0,
    allow_children BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL,
    created_by     UUID,
    updated_at     TIMESTAMPTZ NOT NULL,
    updated_by     UUID,
    PRIMARY KEY (instance, subject_id)
);

-- The notes for registrants and the payment link of a subject per language.
CREATE TABLE wt_registration_subject_text (
    instance     VARCHAR(64) NOT NULL,
    subject_id   VARCHAR(64) NOT NULL,
    locale       VARCHAR(35) NOT NULL,
    notes        TEXT NOT NULL DEFAULT '',
    payment_link VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (instance, subject_id, locale),
    FOREIGN KEY (instance, subject_id) REFERENCES wt_registration_subject (instance, subject_id) ON DELETE CASCADE
);

-- Registrations are visitor submissions; cancelled ones are kept with
-- cancelled_at set and no longer take places. Deleted ones are in the
-- admin's trash (deleted_at) and take no places either. Codes stay unique
-- in the trash too, so a restored registration keeps its code.
CREATE TABLE wt_registration (
    id             UUID PRIMARY KEY,
    instance       VARCHAR(64) NOT NULL,
    subject_id     VARCHAR(64) NOT NULL,
    code           VARCHAR(9) NOT NULL UNIQUE,
    name           VARCHAR(200) NOT NULL,
    email          VARCHAR(254) NOT NULL,
    adults         INTEGER NOT NULL DEFAULT 0,
    children       INTEGER NOT NULL DEFAULT 0,
    total          BIGINT NOT NULL DEFAULT 0,
    payment_status VARCHAR(20) NOT NULL DEFAULT 'none',
    notes          TEXT NOT NULL DEFAULT '',
    admin_notes    TEXT NOT NULL DEFAULT '',
    locale         VARCHAR(35) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    created_by     UUID,
    updated_at     TIMESTAMPTZ NOT NULL,
    updated_by     UUID,
    deleted_at     TIMESTAMPTZ,
    deleted_by     UUID,
    cancelled_at   TIMESTAMPTZ
);

CREATE INDEX wt_registration_subject_list ON wt_registration (instance, subject_id, created_at);
