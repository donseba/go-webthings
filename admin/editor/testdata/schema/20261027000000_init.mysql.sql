-- Fixture schema for the editor's tests: notes keep revisions, cards are a
-- collection (see package content).
CREATE TABLE t_note (
    id                    VARCHAR(36) PRIMARY KEY,
    instance              VARCHAR(64) NOT NULL,
    published_revision_id VARCHAR(36) NULL,
    scheduled_revision_id VARCHAR(36) NULL,
    publish_at            DATETIME(6) NULL,
    unpublish_at          DATETIME(6) NULL,
    created_at            DATETIME(6) NOT NULL,
    created_by            VARCHAR(36) NULL,
    updated_at            DATETIME(6) NOT NULL,
    updated_by            VARCHAR(36) NULL,
    deleted_at            DATETIME(6) NULL,
    deleted_by            VARCHAR(36) NULL
) ENGINE=InnoDB;

CREATE TABLE t_note_revision (
    id           VARCHAR(36) PRIMARY KEY,
    entity_id    VARCHAR(36) NOT NULL,
    revision_no  INTEGER NOT NULL,
    author       VARCHAR(36) NULL,
    created_at   DATETIME(6) NOT NULL,
    note         TEXT NOT NULL,
    review_state VARCHAR(16) NOT NULL DEFAULT 'draft',
    submitted_at DATETIME(6) NULL,
    submitted_by VARCHAR(36) NULL,
    reviewed_at  DATETIME(6) NULL,
    reviewed_by  VARCHAR(36) NULL,
    review_note  VARCHAR(500) NOT NULL DEFAULT '',
    UNIQUE (entity_id, revision_no),
    FOREIGN KEY (entity_id) REFERENCES t_note (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE t_note_translation (
    revision_id VARCHAR(36) NOT NULL,
    locale      VARCHAR(35) NOT NULL,
    body        TEXT NOT NULL,
    PRIMARY KEY (revision_id, locale),
    FOREIGN KEY (revision_id) REFERENCES t_note_revision (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE t_card (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL
) ENGINE=InnoDB;

CREATE TABLE t_card_translation (
    item_id VARCHAR(36) NOT NULL,
    locale  VARCHAR(35) NOT NULL,
    body    TEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES t_card (id) ON DELETE CASCADE
) ENGINE=InnoDB;
