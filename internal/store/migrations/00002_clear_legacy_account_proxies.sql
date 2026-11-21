-- +goose Up
UPDATE accounts SET proxy_enc = NULL WHERE proxy_enc IS NOT NULL;
