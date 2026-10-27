-- Fixture schema for content tests. Media tables mirror the media package.
CREATE TABLE wt_media_asset (
    id         VARCHAR(36) PRIMARY KEY,
    deleted_at DATETIME(6) NULL
) ENGINE=InnoDB;

CREATE TABLE wt_media_usage (
    asset_id VARCHAR(36) NOT NULL,
    owner    VARCHAR(100) NOT NULL,
    owner_id VARCHAR(100) NOT NULL,
    PRIMARY KEY (asset_id, owner, owner_id),
    FOREIGN KEY (asset_id) REFERENCES wt_media_asset (id)
) ENGINE=InnoDB;

-- Revisioned content.
CREATE TABLE t_article (
    id                    VARCHAR(36) PRIMARY KEY,
    instance              VARCHAR(64) NOT NULL,
    published_revision_id VARCHAR(36),
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

CREATE TABLE t_article_revision (
    id           VARCHAR(36) PRIMARY KEY,
    entity_id    VARCHAR(36) NOT NULL,
    revision_no  INTEGER NOT NULL,
    author       VARCHAR(36),
    created_at   DATETIME(6) NOT NULL,
    note         TEXT NOT NULL,
    -- The review workflow (see content.Review); the rest of a revision never changes.
    review_state VARCHAR(16) NOT NULL DEFAULT 'draft',
    submitted_at DATETIME(6) NULL,
    submitted_by VARCHAR(36) NULL,
    reviewed_at  DATETIME(6) NULL,
    reviewed_by  VARCHAR(36) NULL,
    review_note  VARCHAR(500) NOT NULL DEFAULT '',
    pinned       BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (entity_id, revision_no),
    FOREIGN KEY (entity_id) REFERENCES t_article (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE t_article_translation (
    revision_id VARCHAR(36) NOT NULL,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    PRIMARY KEY (revision_id, locale),
    FOREIGN KEY (revision_id) REFERENCES t_article_revision (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Collection content.
CREATE TABLE t_quote (
    id          VARCHAR(36) PRIMARY KEY,
    instance    VARCHAR(64) NOT NULL,
    published   BOOLEAN NOT NULL DEFAULT FALSE,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME(6) NOT NULL,
    created_by  VARCHAR(36) NULL,
    updated_at  DATETIME(6) NOT NULL,
    updated_by  VARCHAR(36) NULL,
    deleted_at  DATETIME(6) NULL,
    deleted_by  VARCHAR(36) NULL,
    author      VARCHAR(200) NOT NULL DEFAULT '',
    -- A unique field leaves deleted rows out: MySQL has no partial indexes,
    -- but a unique index allows many NULLs.
    live_author VARCHAR(200) AS (CASE WHEN deleted_at IS NULL THEN author END) STORED,
    UNIQUE (instance, live_author)
) ENGINE=InnoDB;

CREATE TABLE t_quote_translation (
    item_id VARCHAR(36) NOT NULL,
    locale  VARCHAR(35) NOT NULL,
    slug    VARCHAR(190) NOT NULL DEFAULT '',
    text    TEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES t_quote (id) ON DELETE CASCADE
) ENGINE=InnoDB;
