-- Polls are a collection (see package content). Option IDs are a JSON array
-- shared by all languages (UUIDs from the editor, or any IDs of at most 36
-- characters from code); each translation holds the option labels in the
-- same order.
CREATE TABLE wt_poll (
    id           VARCHAR(36) PRIMARY KEY,
    instance     VARCHAR(64) NOT NULL,
    published    BOOLEAN NOT NULL DEFAULT FALSE,
    position     INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME(6) NOT NULL,
    created_by   VARCHAR(36) NULL,
    updated_at   DATETIME(6) NOT NULL,
    updated_by   VARCHAR(36) NULL,
    deleted_at   DATETIME(6) NULL,
    deleted_by   VARCHAR(36) NULL,
    option_ids   TEXT NOT NULL,
    closes_at    DATETIME(6) NULL,
    show_results BOOLEAN NOT NULL DEFAULT FALSE,
    INDEX wt_poll_instance (instance, position)
) ENGINE=InnoDB;

CREATE TABLE wt_poll_translation (
    item_id       VARCHAR(36) NOT NULL,
    locale        VARCHAR(35) NOT NULL,
    question      VARCHAR(500) NOT NULL DEFAULT '',
    slug          VARCHAR(190) NOT NULL DEFAULT '',
    option_labels TEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    INDEX wt_poll_slug (locale, slug),
    FOREIGN KEY (item_id) REFERENCES wt_poll (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- One vote per visitor and poll. visitor is the SHA-256 of the visitor's id
-- (see package polls), never the id or address itself.
CREATE TABLE wt_poll_vote (
    poll_id    VARCHAR(36) NOT NULL,
    instance   VARCHAR(64) NOT NULL,
    option_id  VARCHAR(36) NOT NULL,
    visitor    CHAR(64) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (poll_id, visitor),
    INDEX wt_poll_vote_instance (instance, visitor),
    FOREIGN KEY (poll_id) REFERENCES wt_poll (id) ON DELETE CASCADE
) ENGINE=InnoDB;
