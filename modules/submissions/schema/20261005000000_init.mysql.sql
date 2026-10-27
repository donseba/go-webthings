-- Visitors' proposals for a news item. media_id is the uploaded image, if any;
-- post_id is the draft post an accepted proposal became. Status is new,
-- accepted or rejected; decided_by is the editor who accepted or rejected it.
-- Deleting moves a proposal to the trash (deleted_at); its post stays.
CREATE TABLE wt_submission (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    name       VARCHAR(200) NOT NULL,
    email      VARCHAR(254) NOT NULL,
    title      VARCHAR(200) NOT NULL,
    body       TEXT NOT NULL,
    media_id   VARCHAR(36) NULL,
    locale     VARCHAR(35) NOT NULL,
    status     VARCHAR(16) NOT NULL DEFAULT 'new',
    post_id    VARCHAR(36) NULL,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL,
    decided_at DATETIME(6) NULL,
    decided_by VARCHAR(36) NULL,
    INDEX wt_submission_instance (instance, status, created_at)
) ENGINE=InnoDB;

-- Settings per instance: who is notified, and whether submitters get a
-- confirmation and hear about the decision. The texts of the mails are mail
-- templates (see package submissions).
CREATE TABLE wt_submission_setting (
    instance      VARCHAR(64) PRIMARY KEY,
    notify        VARCHAR(254) NOT NULL DEFAULT '',
    confirm_mail  BOOLEAN NOT NULL DEFAULT FALSE,
    decision_mail BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at    DATETIME(6) NOT NULL,
    updated_by    VARCHAR(36) NULL
) ENGINE=InnoDB;
