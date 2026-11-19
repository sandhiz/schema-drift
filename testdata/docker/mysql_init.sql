-- MySQL 8 integration seed (Source A / Cross-engine comparison)
CREATE TABLE users (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(120) NOT NULL,
    full_name VARCHAR(80) NOT NULL,
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    legacy_code CHAR(10) NULL
);

CREATE TABLE orders (
    id CHAR(36) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    total_amount DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    notes TEXT NULL
);

CREATE TABLE legacy_sessions (
    session_id VARCHAR(64) PRIMARY KEY,
    expires_at DATETIME NOT NULL
);

CREATE TABLE temp_migration_stage (
    id INT PRIMARY KEY
);
