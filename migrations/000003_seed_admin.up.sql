-- 000003_seed_admin.up.sql — Seed the first admin user for development.
-- Password: admin123 (bcrypt hash below)
-- In production, change this password immediately after first login.

INSERT INTO users (username, password_hash, display_name, status)
VALUES (
    'admin',
    '$2a$10$bz3qqxgmzD2u7Yd0InN8r.fEiT7VTe7VSyCPh420v06dvCAkCTGAG',
    'System Admin',
    'active'
) ON CONFLICT (username) DO NOTHING;

-- Grant admin role to the seeded user
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id FROM users u, roles r
WHERE u.username = 'admin' AND r.name = 'admin'
ON CONFLICT (user_id, role_id) DO NOTHING;
