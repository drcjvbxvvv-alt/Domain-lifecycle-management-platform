-- migrations/000004_cdn_domain_bindings.down.sql

DROP TABLE IF EXISTS cdn_content_tasks;
DROP TABLE IF EXISTS cdn_domain_configs;
DROP TABLE IF EXISTS domain_cdn_bindings;
