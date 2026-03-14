-- Initial schema for gomail-core
-- Creates the core tables: domains, users, mailboxes, messages

CREATE TABLE IF NOT EXISTS domains (
    id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(255) NOT NULL UNIQUE,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    domain_id     BIGINT UNSIGNED NOT NULL,
    username      VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    quota_bytes   BIGINT NOT NULL DEFAULT 1073741824, -- 1 GB default quota
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_user_domain (domain_id, username),
    FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS mailboxes (
    id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id      BIGINT UNSIGNED NOT NULL,
    name         VARCHAR(500) NOT NULL,
    uid_validity INT UNSIGNED NOT NULL DEFAULT 1,
    uid_next     INT UNSIGNED NOT NULL DEFAULT 1,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_mailbox_user (user_id, name),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS messages (
    id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    mailbox_id    BIGINT UNSIGNED NOT NULL,
    uid           INT UNSIGNED NOT NULL,
    flags         SET('\\Seen','\\Answered','\\Flagged','\\Deleted','\\Draft') NOT NULL DEFAULT '',
    size_bytes    INT UNSIGNED NOT NULL,
    raw_key       VARCHAR(1000) NOT NULL, -- Redis key or object storage path for the raw message body
    envelope_from VARCHAR(500) NOT NULL DEFAULT '',
    envelope_to   VARCHAR(500) NOT NULL DEFAULT '',
    subject       VARCHAR(1000) NOT NULL DEFAULT '',
    internal_date TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_mailbox_uid (mailbox_id, uid),
    INDEX idx_internal_date (internal_date),
    FOREIGN KEY (mailbox_id) REFERENCES mailboxes(id) ON DELETE CASCADE
);

-- Insert a default domain and test user for development
INSERT INTO domains (name) VALUES ('gomail.local');

INSERT INTO users (domain_id, username, password_hash, quota_bytes)
VALUES (1, 'test', '$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi', 1073741824);
-- password is: password (bcrypt hash, for development only)

INSERT INTO mailboxes (user_id, name, uid_validity, uid_next)
VALUES (1, 'INBOX', 1, 1),
       (1, 'Sent', 1, 1),
       (1, 'Trash', 1, 1),
       (1, 'Drafts', 1, 1);
