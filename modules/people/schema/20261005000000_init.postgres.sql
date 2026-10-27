-- People are a collection (see package content). The slug is the same in every
-- language and unique per instance.
CREATE TABLE wt_person (
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
    slug       VARCHAR(190) NOT NULL,
    name       VARCHAR(200) NOT NULL DEFAULT '',
    photo_id   UUID,
    email      VARCHAR(254) NOT NULL DEFAULT '',
    phone      VARCHAR(50) NOT NULL DEFAULT '',
    website    VARCHAR(500) NOT NULL DEFAULT '',
    linkedin   VARCHAR(500) NOT NULL DEFAULT ''
);

CREATE INDEX wt_person_instance ON wt_person (instance, position);
-- Deleted rows keep no slug, so a new item can take it.
CREATE UNIQUE INDEX wt_person_slug ON wt_person (instance, slug) WHERE deleted_at IS NULL;

CREATE TABLE wt_person_translation (
    item_id    UUID NOT NULL REFERENCES wt_person (id) ON DELETE CASCADE,
    locale     VARCHAR(35) NOT NULL,
    group_name VARCHAR(200) NOT NULL DEFAULT '',
    role_title VARCHAR(200) NOT NULL DEFAULT '',
    bio        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
