package repository

const (
	registerNewUser           = "INSERT INTO users (user_id, password_hash) VALUES ($1, $2) ON CONFLICT DO NOTHING RETURNING user_id"
	getUserInformationByLogin = "SELECT user_id, password_hash FROM users WHERE user_login = $1"
)
