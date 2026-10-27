-- Form definitions are a collection (see package content); an instance holds
-- one form. Fields and labels are JSON; notify is the address that receives
-- new submissions.
CREATE TABLE wt_forms_form (
    id          VARCHAR(36) PRIMARY KEY,
    instance    VARCHAR(64) NOT NULL,
    published   BOOLEAN NOT NULL DEFAULT FALSE,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME(6) NOT NULL,
    created_by  VARCHAR(36) NULL,
    updated_at  DATETIME(6) NOT NULL,
    updated_by  VARCHAR(36) NULL,
    deleted_at  DATETIME(6) NULL,
    deleted_by  VARCHAR(36) NULL,
    form_fields TEXT NOT NULL,
    notify      VARCHAR(254) NOT NULL DEFAULT '',
    INDEX wt_forms_form_instance (instance)
) ENGINE=InnoDB;

CREATE TABLE wt_forms_form_translation (
    item_id      VARCHAR(36) NOT NULL,
    locale       VARCHAR(35) NOT NULL,
    title        VARCHAR(200) NOT NULL DEFAULT '',
    intro        TEXT NOT NULL,
    success      TEXT NOT NULL,
    field_labels TEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_forms_form (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Submissions belong to the instance, so they outlive changes to the form.
-- Answers are JSON with the labels the visitor saw. Status is new or archived.
CREATE TABLE wt_forms_submission (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    locale     VARCHAR(35) NOT NULL,
    status     VARCHAR(16) NOT NULL DEFAULT 'new',
    answers    MEDIUMTEXT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL,
    INDEX wt_forms_submission_instance (instance, created_at),
    INDEX wt_forms_submission_status (instance, status, created_at)
) ENGINE=InnoDB;
