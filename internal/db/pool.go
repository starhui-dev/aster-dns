package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolConfig struct {
	MaxConnections    int32
	MinConnections    int32
	MaxConnectionAge  time.Duration
	MaxConnectionIdle time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

func OpenPool(ctx context.Context, databaseURL string, cfg PoolConfig) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("parse PostgreSQL configuration")
	}

	poolConfig.MaxConns = cfg.MaxConnections
	poolConfig.MinConns = cfg.MinConnections
	poolConfig.MaxConnLifetime = cfg.MaxConnectionAge
	poolConfig.MaxConnIdleTime = cfg.MaxConnectionIdle
	poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "aster-dns"

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("create PostgreSQL pool")
	}

	pingContext, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		pool.Close()
		return nil, errors.New("connect to PostgreSQL")
	}

	return pool, nil
}
