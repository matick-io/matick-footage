-- +goose Up

-- Recordings live in sources: folders or buckets the app watches, and the
-- library it owns. A recording's path is relative to its source. Existing rows
-- came from the one footage folder, the default watched source.
ALTER TABLE recordings ADD COLUMN source text NOT NULL DEFAULT 'footage';
ALTER TABLE recordings ALTER COLUMN source DROP DEFAULT;
ALTER TABLE recordings DROP CONSTRAINT recordings_path_key;
ALTER TABLE recordings ADD CONSTRAINT recordings_source_path_key UNIQUE (source, path);

-- +goose Down
ALTER TABLE recordings DROP CONSTRAINT recordings_source_path_key;
ALTER TABLE recordings ADD CONSTRAINT recordings_path_key UNIQUE (path);
ALTER TABLE recordings DROP COLUMN source;
