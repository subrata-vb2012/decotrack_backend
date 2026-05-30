package database

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InitDB initializes a connection pool to PostgreSQL using pgxpool.
func InitDB(connString string) (*pgxpool.Pool, error) {
	if connString == "" {
		return nil, errors.New("empty database connection string")
	}

	log.Println("Initializing PostgreSQL database connection pool...")

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}

	// Configure pool sizing and connection settings
	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnIdleTime = 15 * time.Minute
	config.MaxConnLifetime = 1 * time.Hour

	// Initialize connection pool
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	// Verify the connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	log.Println("PostgreSQL connection pool successfully established and pinged.")
	return pool, nil
}
