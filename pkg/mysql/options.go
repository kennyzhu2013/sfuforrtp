package mysql

import "time"

// Option -.
type Option func(*Mysql)

// MaxPoolSize -.
func MaxPoolSize(size int) Option {
	return func(c *Mysql) {
		c.maxOpenCons = size
	}
}

// ConnAttempts -.
func MaxIdleCons(cons int) Option {
	return func(c *Mysql) {
		c.maxIdleCons = cons
	}
}

// ConnTimeout -.
func IdleTimeout(timeout time.Duration) Option {
	return func(c *Mysql) {
		c.idleTimeout = timeout
	}
}
