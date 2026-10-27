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
    closes_at      DATETIME(6) NULL,
    adult_price    BIGINT NOT NULL DEFAULT 0,
    child_price    BIGINT NOT NULL DEFAULT 0,
    allow_children BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     DATETIME(6) NOT NULL,
    created_by     VARCHAR(36) NULL,
    updated_at     DATETIME(6) NOT NULL,
    updated_by     VARCHAR(36) NULL,
    PRIMARY KEY (instance, subject_id)
) ENGINE=InnoDB;

-- The notes for registrants and the payment link of a subject per language.
CREATE TABLE wt_registration_subject_text (
    instance     VARCHAR(64) NOT NULL,
    subject_id   VARCHAR(64) NOT NULL,
    locale       VARCHAR(35) NOT NULL,
    notes        TEXT NOT NULL,
    payment_link VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (instance, subject_id, locale),
    FOREIGN KEY (instance, subject_id) REFERENCES wt_registration_subject (instance, subject_id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Registrations are visitor submissions; cancelled ones are kept with
-- cancelled_at set and no longer take places. Deleted ones are in the
-- admin's trash (deleted_at) and take no places either. Codes stay unique
-- in the trash too, so a restored registration keeps its code.
CREATE TABLE wt_registration (
    id             VARCHAR(36) PRIMARY KEY,
    instance       VARCHAR(64) NOT NULL,
    subject_id     VARCHAR(64) NOT NULL,
    code           VARCHAR(9) NOT NULL UNIQUE,
    name           VARCHAR(200) NOT NULL,
    email          VARCHAR(254) NOT NULL,
    adults         INTEGER NOT NULL DEFAULT 0,
    children       INTEGER NOT NULL DEFAULT 0,
    total          BIGINT NOT NULL DEFAULT 0,
    payment_status VARCHAR(20) NOT NULL DEFAULT 'none',
    notes          TEXT NOT NULL,
    admin_notes    TEXT NOT NULL,
    locale         VARCHAR(35) NOT NULL,
    created_at     DATETIME(6) NOT NULL,
    created_by     VARCHAR(36) NULL,
    updated_at     DATETIME(6) NOT NULL,
    updated_by     VARCHAR(36) NULL,
    deleted_at     DATETIME(6) NULL,
    deleted_by     VARCHAR(36) NULL,
    cancelled_at   DATETIME(6) NULL,
    INDEX wt_registration_subject_list (instance, subject_id, created_at)
) ENGINE=InnoDB;
