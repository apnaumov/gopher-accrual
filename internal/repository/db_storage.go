package repository

import (
	"context"
	"database/sql"
	"errors"

	gopheraccrual "github.com/apnaumov/gopher-accrual.git"
	"github.com/apnaumov/gopher-accrual.git/internal/model"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

var (
	ErrNotFound        = errors.New("can't find record")
	ErrInsertCollision = errors.New("collision while inserting record")
)

type DBStorage struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewDBStorage(connStr string, logger *zap.Logger) (*DBStorage, error) {
	err := runMigrations(connStr)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, err
	}

	dbStorage := &DBStorage{
		db:     db,
		logger: logger,
	}

	return dbStorage, nil
}

func (storage *DBStorage) CreateNewUser(ctx context.Context, login string, password_hash string) (uint64, error) {
	row := storage.db.QueryRowContext(ctx, registerNewUser, login, password_hash)

	var userID uint64

	err := row.Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInsertCollision
		}
		return 0, err
	}

	return userID, nil
}

func (storage *DBStorage) GetUserInformation(ctx context.Context, login string) (uint64, string, error) {
	row := storage.db.QueryRowContext(ctx, getUserInformationByLogin, login)

	var userID uint64
	var passwordHash string

	err := row.Scan(&userID, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", ErrNotFound
		}
		return 0, "", err
	}

	return userID, passwordHash, nil
}

// CreateOrder сохраняет заказ, если он ещё не был загружен.
// Возвращает id владельца заказа и признак того, что заказ был создан этим вызовом.
func (storage *DBStorage) CreateOrder(ctx context.Context, orderNumber model.OrderNumber, userID uint64) (uint64, bool, error) {
	row := storage.db.QueryRowContext(ctx, createOrder, orderNumber, userID, model.OrderStatusNew)

	var ownerID uint64
	var created bool

	err := row.Scan(&ownerID, &created)
	if err == nil {
		return ownerID, created, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	// Конкурентная вставка того же заказа закоммитилась после снимка запроса — перечитываем владельца
	err = storage.db.QueryRowContext(ctx, getOrderOwner, orderNumber).Scan(&ownerID)
	if err != nil {
		return 0, false, err
	}

	return ownerID, false, nil
}

func (storage *DBStorage) OnServerShutdown() error {
	return storage.db.Close()
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
	dbDriver, err := pgx.WithInstance(db, &pgx.Config{})
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
