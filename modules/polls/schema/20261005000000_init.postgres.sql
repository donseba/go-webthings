-- Polls are a collection (see package content). Option IDs are a JSON array
-- shared by all languages (UUIDs from the editor, or any IDs of at most 36
-- characters from code); each translation holds the option labels in the
-- same order.
CREATE TABLE wt_poll (
    id           UUID PRIMARY KEY,
    instance     VARCHAR(64) NOT NULL,
    published    BOOLEAN NOT NULL DEFAULT FALSE,
    position     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL,
    created_by   UUID,
    updated_at   TIMESTAMPTZ NOT NULL,
    updated_by   UUID,
    deleted_at   TIMESTAMPTZ,
    deleted_by   UUID,
    option_ids   TEXT NOT NULL DEFAULT '[]',
    closes_at    TIMESTAMPTZ,
    show_results BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX wt_poll_instance ON wt_poll (instance, position);

CREATE TABLE wt_poll_translation (
    item_id       UUID NOT NULL REFERENCES wt_poll (id) ON DELETE CASCADE,
    locale        VARCHAR(35) NOT NULL,
    question      VARCHAR(500) NOT NULL DEFAULT '',
    slug          VARCHAR(190) NOT NULL DEFAULT '',
    option_labels TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (item_id, locale)
);

CREATE INDEX wt_poll_slug ON wt_poll_translation (locale, slug);

-- One vote per visitor and poll. visitor is the SHA-256 of the visitor's id
-- (see package polls), never the id or address itself.
CREATE TABLE wt_poll_vote (
    poll_id    UUID NOT NULL REFERENCES wt_poll (id) ON DELETE CASCADE,
    instance   VARCHAR(64) NOT NULL,
    option_id  VARCHAR(36) NOT NULL,
    visitor    CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (poll_id, visitor)
);

CREATE INDEX wt_poll_vote_instance ON wt_poll_vote (instance, visitor);
