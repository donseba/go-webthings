CREATE TABLE wt_media_asset (
    id          UUID PRIMARY KEY,
    storage_key VARCHAR(120) NOT NULL UNIQUE,
    filename    VARCHAR(255) NOT NULL,
    mime_type   VARCHAR(100) NOT NULL,
    size_bytes  BIGINT NOT NULL,
    width       INTEGER NOT NULL DEFAULT 0,
    height      INTEGER NOT NULL DEFAULT 0,
    -- The point crops keep in view, as fractions of the upright width and
    -- height from the top left (see media.FocalPoint).
    focal_x     DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    focal_y     DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    checksum    VARCHAR(64) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  UUID,
    updated_at  TIMESTAMPTZ NOT NULL,
    updated_by  UUID,
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID
);

CREATE TABLE wt_media_translation (
    asset_id UUID NOT NULL REFERENCES wt_media_asset (id) ON DELETE CASCADE,
    locale   VARCHAR(35) NOT NULL,
    title    VARCHAR(200) NOT NULL DEFAULT '',
    alt_text TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (asset_id, locale)
);

-- Which content row references which asset. Content stores maintain it; the
-- foreign key stops deleting assets that are in use.
CREATE TABLE wt_media_usage (
    asset_id UUID NOT NULL REFERENCES wt_media_asset (id),
    owner    VARCHAR(100) NOT NULL,
    owner_id VARCHAR(100) NOT NULL,
    PRIMARY KEY (asset_id, owner, owner_id)
);

CREATE INDEX wt_media_usage_owner ON wt_media_usage (owner, owner_id);

-- Resized copies of images (see media.Variant), deleted with their asset.
CREATE TABLE wt_media_variant (
    storage_key VARCHAR(120) PRIMARY KEY,
    asset_id    UUID NOT NULL REFERENCES wt_media_asset (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX wt_media_variant_asset ON wt_media_variant (asset_id);
