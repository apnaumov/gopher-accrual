CREATE TABLE IF NOT EXISTS users (
	user_id SERIAL NOT NULL PRIMARY KEY,
	user_login TEXT NOT NULL,
	password_hash TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_login ON users(user_login);