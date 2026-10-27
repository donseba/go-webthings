-- Banners are a collection (see package content). Times are UTC.
CREATE TABLE wt_banner (
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
    image_id   UUID,
    tone       VARCHAR(20) NOT NULL DEFAULT 'info',
    starts_at  TIMESTAMPTZ,
    ends_at    TIMESTAMPTZ
);

CREATE INDEX wt_banner_instance ON wt_banner (instance, position);

CREATE TABLE wt_banner_translation (
    item_id      UUID NOT NULL REFERENCES wt_banner (id) ON DELETE CASCADE,
    locale       VARCHAR(35) NOT NULL,
    title        VARCHAR(200) NOT NULL DEFAULT '',
    subtitle     VARCHAR(500) NOT NULL DEFAULT '',
    body         TEXT NOT NULL DEFAULT '',
    button_label VARCHAR(100) NOT NULL DEFAULT '',
    button_url   VARCHAR(2000) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
