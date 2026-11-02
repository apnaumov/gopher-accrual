package repository

import (
	"context"
	"database/sql"
	"errors"

	gopheraccrual "github.com/apnaumov/gopher-accrual.git"
	"github.com/apnaumov/gopher-accrual.git/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

var (
	ErrNotFound             = errors.New("can't find record")
	ErrInsertCollision      = errors.New("collision while inserting record")
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrStatusConvertInvalid = errors.New("can't convert order status name to status number")
)

type DBStorage struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewDBStorage(connStr string, logger *zap.Logger) (*DBStorage, error) {
	err := runMigrations(connStr)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, err
	}

	dbStorage := &DBStorage{
		pool:   pool,
		logger: logger,
	}

	return dbStorage, nil
}

func (storage *DBStorage) CreateNewUser(ctx context.Context, login string, passwordHash string) (uint64, error) {
	row := storage.pool.QueryRow(ctx, registerNewUser, login, passwordHash)

	var userID uint64

	err := row.Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrInsertCollision
		}
		return 0, err
	}

	return userID, nil
}

func (storage *DBStorage) GetUserInformation(ctx context.Context, login string) (uint64, string, error) {
	row := storage.pool.QueryRow(ctx, getUserInformationByLogin, login)

	var userID uint64
	var passwordHash string

	err := row.Scan(&userID, &passwordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", ErrNotFound
		}
		return 0, "", err
	}

	return userID, passwordHash, nil
}

// CreateOrder сохраняет заказ, если он ещё не был загружен.
// Возвращает id владельца заказа и признак того, что заказ был создан этим вызовом.
func (storage *DBStorage) CreateOrder(ctx context.Context, orderNumber model.OrderNumber, userID uint64) (uint64, bool, error) {
	row := storage.pool.QueryRow(ctx, createOrder, orderNumber, userID, model.OrderStatusNew)

	var ownerID uint64
	var created bool

	err := row.Scan(&ownerID, &created)
	if err != nil {
		return 0, false, err
	}

	return ownerID, created, nil
}

func (storage *DBStorage) UpdateOrder(ctx context.Context, accrualOrder model.AccrualResponse) (int64, error) {
	orderStatus, ok := model.AccrualOrdersNameToStatus[accrualOrder.Status]
	if !ok {
		return 0, ErrStatusConvertInvalid
	}

	tag, err := storage.pool.Exec(ctx, updateOrder, orderStatus, accrualOrder.Accrual, accrualOrder.Order)
	if err != nil {
		return 0, err
	}

	return tag.RowsAffected(), nil
}

func (storage *DBStorage) GetBalance(ctx context.Context, userID uint64) (float64, float64, error) {
	res := storage.pool.QueryRow(ctx, getBalance, userID)

	var balance float64
	var withdrawn float64

	err := res.Scan(&balance, &withdrawn)

	if err != nil {
		return 0, 0, err
	}

	return balance, withdrawn, nil
}

func (storage *DBStorage) ListOrders(ctx context.Context, userID uint64) ([]model.OrderResponse, error) {
	rows, err := storage.pool.Query(ctx, listOrdersByUser, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []model.OrderResponse

	for rows.Next() {
		var order model.OrderResponse
		var status model.OrderStatus
		var accrual *float64

		if err := rows.Scan(&order.Number, &status, &accrual, &order.UploadedAt); err != nil {
			return nil, err
		}

		order.Status = model.OrdersStatusToName[status]
		// accrual отдаём только для обработанных заказов
		if accrual != nil && status == model.OrderStatusProcessed {
			order.Accrual = accrual
		}

		orders = append(orders, order)
	}

	return orders, rows.Err()
}

// Withdraw списывает баллы атомарно: проверка баланса и вставка выполняются под блокировкой строки пользователя
func (storage *DBStorage) Withdraw(ctx context.Context, userID uint64, orderNumber string, sum float64) error {
	tx, err := storage.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var lockedID uint64
	if err := tx.QueryRow(ctx, lockUser, userID).Scan(&lockedID); err != nil {
		return err
	}

	var balance, withdrawn float64
	if err := tx.QueryRow(ctx, getBalance, userID).Scan(&balance, &withdrawn); err != nil {
		return err
	}

	if balance < sum {
		return ErrInsufficientFunds
	}

	if _, err := tx.Exec(ctx, insertWithdraw, orderNumber, userID, sum); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (storage *DBStorage) ListWithdrawals(ctx context.Context, userID uint64) ([]model.WithdrawalResponse, error) {
	rows, err := storage.pool.Query(ctx, listWithdrawalsByUser, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []model.WithdrawalResponse

	for rows.Next() {
		var w model.WithdrawalResponse
		if err := rows.Scan(&w.Order, &w.Sum, &w.ProcessedAt); err != nil {
			return nil, err
		}
		withdrawals = append(withdrawals, w)
	}

	return withdrawals, rows.Err()
}

// GetUnendedOrders возвращает номера заказов в статусах NEW и PROCESSING
func (storage *DBStorage) GetUnendedOrders(ctx context.Context) ([]model.OrderNumber, error) {
	rows, err := storage.pool.Query(ctx, getUnendedOrders, model.OrderStatusNew, model.OrderStatusProcessing)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orderNumbers []model.OrderNumber

	for rows.Next() {
		var orderNumber model.OrderNumber
		if err := rows.Scan(&orderNumber); err != nil {
			return nil, err
		}
		orderNumbers = append(orderNumbers, orderNumber)
	}

	return orderNumbers, rows.Err()
}

func (storage *DBStorage) OnServerShutdown() error {
	storage.pool.Close()
	return nil
}

func runMigrations(connStr string) error {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return err
	}
	// 1. Создаём source из встроенной ФС
	source, err := iofs.New(gopheraccrual.MigrationsFS, "migrations")
	if err != nil {
		return err
	}

	// 2. Создаём database-драйвер из *sql.DB
	dbDriver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		return err
	}

	// 3. Собираем мигратор
	m, err := migrate.NewWithInstance("iofs", source, "pgx", dbDriver)
	if err != nil {
		return err
	}
	defer m.Close()

	// 4. Применяем
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
