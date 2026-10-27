-- Visitors' proposals for a news item. media_id is the uploaded image, if any;
-- post_id is the draft post an accepted proposal became. Status is new,
-- accepted or rejected; decided_by is the editor who accepted or rejected it.
-- Deleting moves a proposal to the trash (deleted_at); its post stays.
CREATE TABLE wt_submission (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    name       TEXT NOT NULL,
    email      TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    media_id   TEXT,
    locale     TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'new',
    post_id    TEXT,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT,
    decided_at TIMESTAMP,
    decided_by TEXT
);

CREATE INDEX wt_submission_instance ON wt_submission (instance, status, created_at);

-- Settings per instance: who is notified, and whether submitters get a
-- confirmation and hear about the decision. The texts of the mails are mail
-- templates (see package submissions).
CREATE TABLE wt_submission_setting (
    instance      TEXT PRIMARY KEY,
    notify        TEXT NOT NULL DEFAULT '',
    confirm_mail  BOOLEAN NOT NULL DEFAULT FALSE,
    decision_mail BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at    TIMESTAMP NOT NULL,
    updated_by    TEXT
);
