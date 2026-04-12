-- 000003_seed_admin.down.sql — Remove seed admin user
DELETE FROM user_roles WHERE user_id = (SELECT id FROM users WHERE username = 'admin');
DELETE FROM users WHERE username = 'admin';
