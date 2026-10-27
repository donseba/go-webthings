-- Form definitions are a collection (see package content); an instance holds
-- one form. Fields and labels are JSON; notify is the address that receives
-- new submissions.
CREATE TABLE wt_forms_form (
    id          UUID PRIMARY KEY,
    instance    VARCHAR(64) NOT NULL,
    published   BOOLEAN NOT NULL DEFAULT FALSE,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  UUID,
    updated_at  TIMESTAMPTZ NOT NULL,
    updated_by  UUID,
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID,
    form_fields TEXT NOT NULL DEFAULT '[]',
    notify      VARCHAR(254) NOT NULL DEFAULT ''
);

CREATE INDEX wt_forms_form_instance ON wt_forms_form (instance);

CREATE TABLE wt_forms_form_translation (
    item_id      UUID NOT NULL REFERENCES wt_forms_form (id) ON DELETE CASCADE,
    locale       VARCHAR(35) NOT NULL,
    title        VARCHAR(200) NOT NULL DEFAULT '',
    intro        TEXT NOT NULL DEFAULT '',
    success      TEXT NOT NULL DEFAULT '',
    field_labels TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (item_id, locale)
);

-- Submissions belong to the instance, so they outlive changes to the form.
-- Answers are JSON with the labels the visitor saw. Status is new or archived.
CREATE TABLE wt_forms_submission (
    id         UUID PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    locale     VARCHAR(35) NOT NULL,
    status     VARCHAR(16) NOT NULL DEFAULT 'new',
    answers    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    created_by UUID,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID
);

CREATE INDEX wt_forms_submission_instance ON wt_forms_submission (instance, created_at);

CREATE INDEX wt_forms_submission_status ON wt_forms_submission (instance, status, created_at);
