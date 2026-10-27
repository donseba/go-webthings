-- The texts of an instance (signup heading and introduction, mailing footer)
-- are a collection with one item (see package content). The confirmation
-- mail is a mail template.
CREATE TABLE wt_newsletter (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT
);

CREATE INDEX wt_newsletter_instance ON wt_newsletter (instance, position);

CREATE TABLE wt_newsletter_translation (
    item_id         TEXT NOT NULL REFERENCES wt_newsletter (id) ON DELETE CASCADE,
    locale          TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    intro           TEXT NOT NULL DEFAULT '',
    footer          TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);

-- Subscribers sign up as pending and confirm with the token sent to them; the
-- same token is in their unsubscribe links. An address is on the list once,
-- not counting deleted subscribers in the trash, so it can sign up again.
CREATE TABLE wt_newsletter_subscriber (
    id           TEXT PRIMARY KEY,
    instance     TEXT NOT NULL,
    email        TEXT NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    locale       TEXT NOT NULL,
    status       TEXT NOT NULL,
    token        TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMP NOT NULL,
    created_by   TEXT,
    updated_at   TIMESTAMP NOT NULL,
    updated_by   TEXT,
    deleted_at   TIMESTAMP,
    deleted_by   TEXT,
    confirmed_at TIMESTAMP
);

CREATE UNIQUE INDEX wt_newsletter_subscriber_email ON wt_newsletter_subscriber (instance, email) WHERE deleted_at IS NULL;
CREATE INDEX wt_newsletter_subscriber_status ON wt_newsletter_subscriber (instance, status, created_at);
CREATE INDEX wt_newsletter_subscriber_sending ON wt_newsletter_subscriber (instance, status, id);

-- Mailings are drafts until sent_at is set, when sending starts in the
-- background. progress is the ID of the last subscriber handed to the mail
-- sender (subscribers are sent to in ID order), so sending resumes after a
-- restart; total is the audience when sending started, recipients and failed
-- count the messages so far, and finished_at is set when all are done.
-- origin is the site's address for the links in the mail.
CREATE TABLE wt_newsletter_mailing (
    id          TEXT PRIMARY KEY,
    instance    TEXT NOT NULL,
    locale      TEXT NOT NULL DEFAULT '',
    subject     TEXT NOT NULL,
    body        TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    created_by  TEXT,
    updated_at  TIMESTAMP NOT NULL,
    updated_by  TEXT,
    deleted_at  TIMESTAMP,
    deleted_by  TEXT,
    sent_at     TIMESTAMP,
    origin      TEXT NOT NULL DEFAULT '',
    total       INTEGER NOT NULL DEFAULT 0,
    progress    TEXT NOT NULL DEFAULT '',
    recipients  INTEGER NOT NULL DEFAULT 0,
    failed      INTEGER NOT NULL DEFAULT 0,
    finished_at TIMESTAMP
);

CREATE INDEX wt_newsletter_mailing_instance ON wt_newsletter_mailing (instance, created_at);
