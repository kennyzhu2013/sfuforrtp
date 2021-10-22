// Package mysql implements mysql connection.
package mysql

import (
	"fmt"
	//"log"
	"time"

	"github.com/Masterminds/squirrel" // fluent SQL generator for Go
	// "github.com/jackc/pgx/v4/pgxpool"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

const (
	_defaultMaxOpenCons = 100
	_defaultIdleCons    = 10
	_defaultIdleTimeout = time.Second
)

// Mysql -.
type Mysql struct {
	maxOpenCons int
	maxIdleCons int
	idleTimeout time.Duration

	Builder squirrel.StatementBuilderType
	// Pool    *pgxpool.Pool
	Db *sqlx.DB
}

// New -.
func New(url string, opts ...Option) (*Mysql, error) {
	ms := &Mysql{
		maxOpenCons: _defaultMaxOpenCons,
		maxIdleCons: _defaultIdleCons,
		idleTimeout: _defaultIdleTimeout,
	}

	// Custom options
	for _, opt := range opts {
		opt(ms)
	}

	// sqlx support ?.
	ms.Builder = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

	// poolConfig, err := pgxpool.ParseConfig(url)
	var err error
	ms.Db, err = sqlx.Open("mysql", "root:L0v1!@#$@tcp(10.153.90.12:3306)/mid_bussops_prod?charset=utf8")
	if err != nil {
		fmt.Printf("mysql connect failed, detail is [%v]", err.Error())
	}

	if err != nil {
		return nil, fmt.Errorf("mysql - NewPostgres - pgxpool.ParseConfig: %w", err)
	}

    // set man connections and con attempts
	// poolConfig.MaxConns = int32(ms.maxOpenCons)
	ms.Db.SetMaxOpenConns( ms.maxOpenCons)
	ms.Db.SetMaxIdleConns(  ms.maxIdleCons )
	ms.Db.SetConnMaxLifetime( 0 )
	ms.Db.SetConnMaxIdleTime( ms.idleTimeout )

	//for connAttemps > 0 {
	//	ms.Pool, err = pgxpool.ConnectConfig(context.Background(), poolConfig)
	//	if err == nil {
	//		break
	//	}
	//	log.Printf("Mysql is trying to connect, attempts left: %d", ms.maxIdleCons)
	//	time.Sleep(ms.idleTimeout)
	//	connAttemps--
	//}
	if err != nil {
		return nil, fmt.Errorf("mysql - NewMysql - maxIdleCons == 0: %w", err)
	}

	return ms, nil
}

// Close -.
func (p *Mysql) Close() {
	if p.Db != nil {
		p.Db.Close()
	}
}
