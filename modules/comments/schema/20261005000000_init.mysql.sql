-- Visitor comments and guestbook entries. subject is what a comment belongs
-- to, chosen by the template: a post ID, a page path or a fixed word. A reply
-- points at its top-level comment; staff replies are written in the admin.
-- ip_hash is a keyed, daily-changing hash of the address, only for spotting
-- floods; the address itself is never stored.
-- Deleting moves a message to the trash (deleted_at); its replies go along
-- with the same deleted_at and come back with it.
CREATE TABLE wt_comment (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    subject    VARCHAR(200) NOT NULL,
    parent_id  VARCHAR(36) NULL,
    name       VARCHAR(100) NOT NULL,
    email      VARCHAR(254) NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    locale     VARCHAR(35) NOT NULL,
    page_path  VARCHAR(500) NOT NULL DEFAULT '',
    approved   BOOLEAN NOT NULL DEFAULT FALSE,
    is_staff   BOOLEAN NOT NULL DEFAULT FALSE,
    ip_hash    VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL,
    INDEX wt_comment_subject (instance, subject, created_at),
    INDEX wt_comment_review (instance, approved, created_at),
    FOREIGN KEY (parent_id) REFERENCES wt_comment (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Settings per instance; an instance without a row uses the defaults
-- (moderated, open, no notification).
CREATE TABLE wt_comment_setting (
    instance   VARCHAR(64) PRIMARY KEY,
    moderated  BOOLEAN NOT NULL DEFAULT TRUE,
    closed     BOOLEAN NOT NULL DEFAULT FALSE,
    notify     VARCHAR(254) NOT NULL DEFAULT '',
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL
) ENGINE=InnoDB;
