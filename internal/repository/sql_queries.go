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
	updateOrder = "UPDATE orders SET status = $1, accrual = $2 WHERE order_number = $3"
	// Заказы, по которым ещё не получен финальный статус (NEW, PROCESSING)
	getUnendedOrders      = "SELECT order_number FROM orders WHERE status IN ($1, $2) ORDER BY uploaded_at, internal_id"
	getOrderOwner         = "SELECT user_id FROM orders WHERE order_number = $1"
	listOrdersByUser      = "SELECT order_number, status, accrual, uploaded_at FROM orders WHERE user_id = $1 ORDER BY uploaded_at DESC, internal_id DESC"
	listWithdrawalsByUser = "SELECT order_number, sum, processed_at FROM withdrawals WHERE user_id = $1 ORDER BY processed_at DESC, internal_id DESC"
	// Блокирует строку пользователя, чтобы параллельные списания не увели баланс в минус
	lockUser       = "SELECT user_id FROM users WHERE user_id = $1 FOR UPDATE"
	insertWithdraw = "INSERT INTO withdrawals (order_number, user_id, sum) VALUES ($1, $2, $3)"

	getBalance = `WITH withdrawal_sum AS (
						SELECT COALESCE((SELECT SUM(sum) FROM withdrawals WHERE user_id = $1), 0) AS withdrawn
					)
					SELECT COALESCE((SELECT SUM(accrual) FROM orders WHERE user_id = $1), 0) - withdrawn AS balance, withdrawn FROM withdrawal_sum`
)
