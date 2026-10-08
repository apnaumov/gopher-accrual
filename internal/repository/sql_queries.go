package repository

const (
	registerNewUser           = "INSERT INTO users (user_login, password_hash) VALUES ($1, $2) ON CONFLICT DO NOTHING RETURNING user_id"
	getUserInformationByLogin = "SELECT user_id, password_hash FROM users WHERE user_login = $1"

	// Возвращает владельца заказа и признак того, что заказ был создан этим запросом
	createOrder = `WITH inserted AS (
		INSERT INTO orders (order_number, user_id, status) VALUES ($1, $2, $3)
		ON CONFLICT (order_number) DO NOTHING
		RETURNING user_id
	)
	SELECT user_id, true FROM inserted
	UNION ALL
	SELECT user_id, false FROM orders WHERE order_number = $1
	LIMIT 1`
	updateOrder   = "UPDATE orders SET status = $1, accrual = $2 WHERE order_number = $3"
	getOrderOwner = "SELECT user_id FROM orders WHERE order_number = $1"
)
