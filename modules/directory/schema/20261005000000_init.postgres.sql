-- Categories and entries are collections (see package content). Slugs are not
-- translated and are unique per instance.
CREATE TABLE wt_directory_category (
    id         UUID PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    created_by UUID,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID,
    slug       VARCHAR(190) NOT NULL
);

-- Deleted rows keep no slug, so a new item can take it.
CREATE UNIQUE INDEX wt_directory_category_slug ON wt_directory_category (instance, slug) WHERE deleted_at IS NULL;

CREATE TABLE wt_directory_category_translation (
    item_id UUID NOT NULL REFERENCES wt_directory_category (id) ON DELETE CASCADE,
    locale  VARCHAR(35) NOT NULL,
    name    VARCHAR(200) NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);

-- A category with entries cannot be deleted: its entries would disappear from
-- the site. Editors move or delete them first.
CREATE TABLE wt_directory_entry (
    id            UUID PRIMARY KEY,
    instance      VARCHAR(64) NOT NULL,
    published     BOOLEAN NOT NULL DEFAULT FALSE,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL,
    created_by    UUID,
    updated_at    TIMESTAMPTZ NOT NULL,
    updated_by    UUID,
    deleted_at    TIMESTAMPTZ,
    deleted_by    UUID,
    category_id   UUID REFERENCES wt_directory_category (id),
    slug          VARCHAR(190) NOT NULL,
    name          VARCHAR(200) NOT NULL DEFAULT '',
    image_id      UUID,
    website       VARCHAR(500) NOT NULL DEFAULT '',
    email         VARCHAR(254) NOT NULL DEFAULT '',
    phone         VARCHAR(50) NOT NULL DEFAULT '',
    street        VARCHAR(200) NOT NULL DEFAULT '',
    postcode      VARCHAR(20) NOT NULL DEFAULT '',
    city          VARCHAR(100) NOT NULL DEFAULT '',
    latitude      DOUBLE PRECISION,
    longitude     DOUBLE PRECISION,
    opening_hours TEXT NOT NULL DEFAULT ''
);

-- Deleted rows keep no slug, so a new item can take it.
CREATE UNIQUE INDEX wt_directory_entry_slug ON wt_directory_entry (instance, slug) WHERE deleted_at IS NULL;

CREATE INDEX wt_directory_entry_category ON wt_directory_entry (category_id);

CREATE TABLE wt_directory_entry_translation (
    item_id     UUID NOT NULL REFERENCES wt_directory_entry (id) ON DELETE CASCADE,
    locale      VARCHAR(35) NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    keywords    TEXT NOT NULL DEFAULT '',
    hours_note  VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
