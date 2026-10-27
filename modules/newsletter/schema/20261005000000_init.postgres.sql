-- The texts of an instance (signup heading and introduction, mailing footer)
-- are a collection with one item (see package content). The confirmation
-- mail is a mail template.
CREATE TABLE wt_newsletter (
    id         UUID PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    created_by UUID,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID
);

CREATE INDEX wt_newsletter_instance ON wt_newsletter (instance, position);

CREATE TABLE wt_newsletter_translation (
    item_id         UUID NOT NULL REFERENCES wt_newsletter (id) ON DELETE CASCADE,
    locale          VARCHAR(35) NOT NULL,
    title           VARCHAR(200) NOT NULL DEFAULT '',
    intro           TEXT NOT NULL DEFAULT '',
    footer          TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);

-- Subscribers sign up as pending and confirm with the token sent to them; the
-- same token is in their unsubscribe links. An address is on the list once,
-- not counting deleted subscribers in the trash, so it can sign up again.
CREATE TABLE wt_newsletter_subscriber (
    id           UUID PRIMARY KEY,
    instance     VARCHAR(64) NOT NULL,
    email        VARCHAR(254) NOT NULL,
    name         VARCHAR(200) NOT NULL DEFAULT '',
    locale       VARCHAR(35) NOT NULL,
    status       VARCHAR(20) NOT NULL,
    token        VARCHAR(64) NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL,
    created_by   UUID,
    updated_at   TIMESTAMPTZ NOT NULL,
    updated_by   UUID,
    deleted_at   TIMESTAMPTZ,
    deleted_by   UUID,
    confirmed_at TIMESTAMPTZ
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
    id          UUID PRIMARY KEY,
    instance    VARCHAR(64) NOT NULL,
    locale      VARCHAR(35) NOT NULL DEFAULT '',
    subject     VARCHAR(200) NOT NULL,
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  UUID,
    updated_at  TIMESTAMPTZ NOT NULL,
    updated_by  UUID,
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID,
    sent_at     TIMESTAMPTZ,
    origin      VARCHAR(200) NOT NULL DEFAULT '',
    total       INTEGER NOT NULL DEFAULT 0,
    progress    VARCHAR(36) NOT NULL DEFAULT '',
    recipients  INTEGER NOT NULL DEFAULT 0,
    failed      INTEGER NOT NULL DEFAULT 0,
    finished_at TIMESTAMPTZ
);

CREATE INDEX wt_newsletter_mailing_instance ON wt_newsletter_mailing (instance, created_at);
