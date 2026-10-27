-- Form definitions are a collection (see package content); an instance holds
-- one form. Fields and labels are JSON; notify is the address that receives
-- new submissions.
CREATE TABLE wt_forms_form (
    id          TEXT PRIMARY KEY,
    instance    TEXT NOT NULL,
    published   BOOLEAN NOT NULL DEFAULT FALSE,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMP NOT NULL,
    created_by  TEXT,
    updated_at  TIMESTAMP NOT NULL,
    updated_by  TEXT,
    deleted_at  TIMESTAMP,
    deleted_by  TEXT,
    form_fields TEXT NOT NULL DEFAULT '[]',
    notify      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX wt_forms_form_instance ON wt_forms_form (instance);

CREATE TABLE wt_forms_form_translation (
    item_id      TEXT NOT NULL REFERENCES wt_forms_form (id) ON DELETE CASCADE,
    locale       TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    intro        TEXT NOT NULL DEFAULT '',
    success      TEXT NOT NULL DEFAULT '',
    field_labels TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (item_id, locale)
);

-- Submissions belong to the instance, so they outlive changes to the form.
-- Answers are JSON with the labels the visitor saw. Status is new or archived.
CREATE TABLE wt_forms_submission (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    locale     TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'new',
    answers    TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT
);

CREATE INDEX wt_forms_submission_instance ON wt_forms_submission (instance, created_at);

CREATE INDEX wt_forms_submission_status ON wt_forms_submission (instance, status, created_at);
