-- Polls are a collection (see package content). Option IDs are a JSON array
-- shared by all languages (UUIDs from the editor, or any IDs of at most 36
-- characters from code); each translation holds the option labels in the
-- same order.
CREATE TABLE wt_poll (
    id           TEXT PRIMARY KEY,
    instance     TEXT NOT NULL,
    published    BOOLEAN NOT NULL DEFAULT FALSE,
    position     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMP NOT NULL,
    created_by   TEXT,
    updated_at   TIMESTAMP NOT NULL,
    updated_by   TEXT,
    deleted_at   TIMESTAMP,
    deleted_by   TEXT,
    option_ids   TEXT NOT NULL DEFAULT '[]',
    closes_at    TIMESTAMP,
    show_results BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX wt_poll_instance ON wt_poll (instance, position);

CREATE TABLE wt_poll_translation (
    item_id       TEXT NOT NULL REFERENCES wt_poll (id) ON DELETE CASCADE,
    locale        TEXT NOT NULL,
    question      TEXT NOT NULL DEFAULT '',
    slug          TEXT NOT NULL DEFAULT '',
    option_labels TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (item_id, locale)
);

CREATE INDEX wt_poll_slug ON wt_poll_translation (locale, slug);

-- One vote per visitor and poll. visitor is the SHA-256 of the visitor's id
-- (see package polls), never the id or address itself.
CREATE TABLE wt_poll_vote (
    poll_id    TEXT NOT NULL REFERENCES wt_poll (id) ON DELETE CASCADE,
    instance   TEXT NOT NULL,
    option_id  TEXT NOT NULL,
    visitor    TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (poll_id, visitor)
);

CREATE INDEX wt_poll_vote_instance ON wt_poll_vote (instance, visitor);
