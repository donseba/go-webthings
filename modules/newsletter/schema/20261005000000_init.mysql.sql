-- The texts of an instance (signup heading and introduction, mailing footer)
-- are a collection with one item (see package content). The confirmation
-- mail is a mail template.
CREATE TABLE wt_newsletter (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL,
    INDEX wt_newsletter_instance (instance, position)
) ENGINE=InnoDB;

CREATE TABLE wt_newsletter_translation (
    item_id         VARCHAR(36) NOT NULL,
    locale          VARCHAR(35) NOT NULL,
    title           VARCHAR(200) NOT NULL DEFAULT '',
    intro           TEXT NOT NULL,
    footer          TEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_newsletter (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Subscribers sign up as pending and confirm with the token sent to them; the
-- same token is in their unsubscribe links. An address is on the list once,
-- not counting deleted subscribers in the trash, so it can sign up again.
CREATE TABLE wt_newsletter_subscriber (
    id           VARCHAR(36) PRIMARY KEY,
    instance     VARCHAR(64) NOT NULL,
    email        VARCHAR(254) NOT NULL,
    name         VARCHAR(200) NOT NULL DEFAULT '',
    locale       VARCHAR(35) NOT NULL,
    status       VARCHAR(20) NOT NULL,
    token        VARCHAR(64) NOT NULL,
    created_at   DATETIME(6) NOT NULL,
    created_by   VARCHAR(36) NULL,
    updated_at   DATETIME(6) NOT NULL,
    updated_by   VARCHAR(36) NULL,
    deleted_at   DATETIME(6) NULL,
    deleted_by   VARCHAR(36) NULL,
    confirmed_at DATETIME(6) NULL,
    live_email   VARCHAR(254) AS (CASE WHEN deleted_at IS NULL THEN email END) STORED,
    UNIQUE (token),
    UNIQUE INDEX wt_newsletter_subscriber_email (instance, live_email),
    INDEX wt_newsletter_subscriber_status (instance, status, created_at),
    INDEX wt_newsletter_subscriber_sending (instance, status, id)
) ENGINE=InnoDB;

-- Mailings are drafts until sent_at is set, when sending starts in the
-- background. progress is the ID of the last subscriber handed to the mail
-- sender (subscribers are sent to in ID order), so sending resumes after a
-- restart; total is the audience when sending started, recipients and failed
-- count the messages so far, and finished_at is set when all are done.
-- origin is the site's address for the links in the mail.
CREATE TABLE wt_newsletter_mailing (
    id          VARCHAR(36) PRIMARY KEY,
    instance    VARCHAR(64) NOT NULL,
    locale      VARCHAR(35) NOT NULL DEFAULT '',
    subject     VARCHAR(200) NOT NULL,
    body        MEDIUMTEXT NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    created_by  VARCHAR(36) NULL,
    updated_at  DATETIME(6) NOT NULL,
    updated_by  VARCHAR(36) NULL,
    deleted_at  DATETIME(6) NULL,
    deleted_by  VARCHAR(36) NULL,
    sent_at     DATETIME(6) NULL,
    origin      VARCHAR(200) NOT NULL DEFAULT '',
    total       INTEGER NOT NULL DEFAULT 0,
    progress    VARCHAR(36) NOT NULL DEFAULT '',
    recipients  INTEGER NOT NULL DEFAULT 0,
    failed      INTEGER NOT NULL DEFAULT 0,
    finished_at DATETIME(6) NULL,
    INDEX wt_newsletter_mailing_instance (instance, created_at)
) ENGINE=InnoDB;
