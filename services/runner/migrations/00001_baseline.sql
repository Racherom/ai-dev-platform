-- Baseline: no tables yet. It gives goose its version table so later migrations start from a
-- known state. Feature #3 (data model) adds runner's tables as 00002 and up.
-- +goose Up
SELECT 1;

-- +goose Down
SELECT 1;
