CREATE TABLE wt_media_asset (
    id          TEXT PRIMARY KEY,
    storage_key TEXT NOT NULL UNIQUE,
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    width       INTEGER NOT NULL DEFAULT 0,
    height      INTEGER NOT NULL DEFAULT 0,
    -- The point crops keep in view, as fractions of the upright width and
    -- height from the top left (see media.FocalPoint).
    focal_x     REAL NOT NULL DEFAULT 0.5,
    focal_y     REAL NOT NULL DEFAULT 0.5,
    checksum    TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    created_by  TEXT,
    updated_at  TIMESTAMP NOT NULL,
    updated_by  TEXT,
    deleted_at  TIMESTAMP,
    deleted_by  TEXT
);

CREATE TABLE wt_media_translation (
    asset_id TEXT NOT NULL REFERENCES wt_media_asset (id) ON DELETE CASCADE,
    locale   TEXT NOT NULL,
    title    TEXT NOT NULL DEFAULT '',
    alt_text TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (asset_id, locale)
);

-- Which content row references which asset. Content stores maintain it; the
-- foreign key stops deleting assets that are in use.
CREATE TABLE wt_media_usage (
    asset_id TEXT NOT NULL REFERENCES wt_media_asset (id),
    owner    TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    PRIMARY KEY (asset_id, owner, owner_id)
);

CREATE INDEX wt_media_usage_owner ON wt_media_usage (owner, owner_id);

-- Resized copies of images (see media.Variant), deleted with their asset.
CREATE TABLE wt_media_variant (
    storage_key TEXT PRIMARY KEY,
    asset_id    TEXT NOT NULL REFERENCES wt_media_asset (id) ON DELETE CASCADE,
    created_at  TIMESTAMP NOT NULL
);

CREATE INDEX wt_media_variant_asset ON wt_media_variant (asset_id);
