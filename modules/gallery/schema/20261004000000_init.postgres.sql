-- Albums are a collection (see package content).
CREATE TABLE wt_gallery_album (
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
    cover_id   UUID,
    image_ids  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX wt_gallery_album_instance ON wt_gallery_album (instance);

CREATE TABLE wt_gallery_album_translation (
    item_id     UUID NOT NULL REFERENCES wt_gallery_album (id) ON DELETE CASCADE,
    locale      VARCHAR(35) NOT NULL,
    slug        VARCHAR(190) NOT NULL DEFAULT '',
    title       VARCHAR(200) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);

CREATE INDEX wt_gallery_album_slug ON wt_gallery_album_translation (locale, slug);
