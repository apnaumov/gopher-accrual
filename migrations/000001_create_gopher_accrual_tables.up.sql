CREATE TABLE IF NOT EXISTS users (
	user_id SERIAL NOT NULL PRIMARY KEY,
	user_login TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS orders (
	internal_id SERIAL NOT NULL PRIMARY KEY,
	order_number TEXT NOT NULL UNIQUE,
	user_id INTEGER NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
	status SMALLINT NOT NULL CHECK (status >= 1 AND status <= 4),
	accrual NUMERIC(12,2),
	uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS orders_user_id_idx ON orders(user_id);

CREATE TABLE IF NOT EXISTS withdrawals (
	internal_id SERIAL NOT NULL PRIMARY KEY,
	order_number TEXT NOT NULL,
	user_id INTEGER NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
	sum NUMERIC(12,2) NOT NULL,
	processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS withdrawals_user_id_idx ON withdrawals(user_id);