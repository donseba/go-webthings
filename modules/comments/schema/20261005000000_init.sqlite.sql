-- Visitor comments and guestbook entries. subject is what a comment belongs
-- to, chosen by the template: a post ID, a page path or a fixed word. A reply
-- points at its top-level comment; staff replies are written in the admin.
-- ip_hash is a keyed, daily-changing hash of the address, only for spotting
-- floods; the address itself is never stored.
-- Deleting moves a message to the trash (deleted_at); its replies go along
-- with the same deleted_at and come back with it.
CREATE TABLE wt_comment (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    parent_id  TEXT REFERENCES wt_comment (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    locale     TEXT NOT NULL,
    page_path  TEXT NOT NULL DEFAULT '',
    approved   BOOLEAN NOT NULL DEFAULT FALSE,
    is_staff   BOOLEAN NOT NULL DEFAULT FALSE,
    ip_hash    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT
);

CREATE INDEX wt_comment_subject ON wt_comment (instance, subject, created_at);

CREATE INDEX wt_comment_review ON wt_comment (instance, approved, created_at);

CREATE INDEX wt_comment_parent ON wt_comment (parent_id);

-- Settings per instance; an instance without a row uses the defaults
-- (moderated, open, no notification).
CREATE TABLE wt_comment_setting (
    instance   TEXT PRIMARY KEY,
    moderated  BOOLEAN NOT NULL DEFAULT TRUE,
    closed     BOOLEAN NOT NULL DEFAULT FALSE,
    notify     TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT
);
