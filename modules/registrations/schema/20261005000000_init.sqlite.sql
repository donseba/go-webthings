-- Settings per subject (usually an event) that visitors can register for.
-- Prices are in cents. Without closes_at, registration for an event closes
-- when it starts. What registrants read (notes, payment link) is per
-- language in wt_registration_subject_text.
CREATE TABLE wt_registration_subject (
    instance       TEXT NOT NULL,
    subject_id     TEXT NOT NULL,
    title          TEXT NOT NULL DEFAULT '',
    is_open        BOOLEAN NOT NULL DEFAULT FALSE,
    capacity       INTEGER NOT NULL DEFAULT 0,
    closes_at      TIMESTAMP,
    adult_price    INTEGER NOT NULL DEFAULT 0,
    child_price    INTEGER NOT NULL DEFAULT 0,
    allow_children BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMP NOT NULL,
    created_by     TEXT,
    updated_at     TIMESTAMP NOT NULL,
    updated_by     TEXT,
    PRIMARY KEY (instance, subject_id)
);

-- The notes for registrants and the payment link of a subject per language.
CREATE TABLE wt_registration_subject_text (
    instance     TEXT NOT NULL,
    subject_id   TEXT NOT NULL,
    locale       TEXT NOT NULL,
    notes        TEXT NOT NULL DEFAULT '',
    payment_link TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (instance, subject_id, locale),
    FOREIGN KEY (instance, subject_id) REFERENCES wt_registration_subject (instance, subject_id) ON DELETE CASCADE
);

-- Registrations are visitor submissions; cancelled ones are kept with
-- cancelled_at set and no longer take places. Deleted ones are in the
-- admin's trash (deleted_at) and take no places either. Codes stay unique
-- in the trash too, so a restored registration keeps its code.
CREATE TABLE wt_registration (
    id             TEXT PRIMARY KEY,
    instance       TEXT NOT NULL,
    subject_id     TEXT NOT NULL,
    code           TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    email          TEXT NOT NULL,
    adults         INTEGER NOT NULL DEFAULT 0,
    children       INTEGER NOT NULL DEFAULT 0,
    total          INTEGER NOT NULL DEFAULT 0,
    payment_status TEXT NOT NULL DEFAULT 'none',
    notes          TEXT NOT NULL DEFAULT '',
    admin_notes    TEXT NOT NULL DEFAULT '',
    locale         TEXT NOT NULL,
    created_at     TIMESTAMP NOT NULL,
    created_by     TEXT,
    updated_at     TIMESTAMP NOT NULL,
    updated_by     TEXT,
    deleted_at     TIMESTAMP,
    deleted_by     TEXT,
    cancelled_at   TIMESTAMP
);

CREATE INDEX wt_registration_subject_list ON wt_registration (instance, subject_id, created_at);
