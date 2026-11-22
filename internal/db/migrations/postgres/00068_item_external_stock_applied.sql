-- +goose Up
ALTER TABLE item_external_stock ADD COLUMN applied_action TEXT NOT NULL DEFAULT '';
ALTER TABLE item_external_stock ADD COLUMN applied_available INTEGER;
ALTER TABLE item_external_stock ADD COLUMN applied_unix BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE item_external_stock DROP COLUMN applied_unix;
ALTER TABLE item_external_stock DROP COLUMN applied_available;
ALTER TABLE item_external_stock DROP COLUMN applied_action;
