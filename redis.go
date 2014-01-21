package goworker

import (
	"code.google.com/p/vitess/go/pools"
	"errors"
	"github.com/garyburd/redigo/redis"
	"net/url"
	"time"
)

var (
	errorInvalidScheme = errors.New("Invalid Redis database URI scheme.")
	pool               *pools.ResourcePool
)

type redisConn struct {
	redis.Conn
}

func (r *redisConn) Close() {
	_ = r.Conn.Close()
}

// Retrieve a redis pool
func getConnectionPool() *pools.ResourcePool {
	if pool == nil || pool.IsClosed() {
		pool = newRedisPool(uri, connections, connections, time.Minute)
	}
	return pool
}

// Manually defines a connection pool
func setConnectionPool(p *pools.ResourcePool) {
	if !(pool == nil || pool.IsClosed()) {
		pool.Close()
	}
	pool = p
}

func newRedisFactory(uri string) pools.Factory {
	return func() (pools.Resource, error) {
		return redisConnFromUri(uri)
	}
}

func newRedisPool(uri string, capacity int, maxCapacity int, idleTimout time.Duration) *pools.ResourcePool {
	return pools.NewResourcePool(newRedisFactory(uri), capacity, maxCapacity, idleTimout)
}

func redisConnFromUri(uriString string) (*redisConn, error) {
	uri, err := url.Parse(uriString)
	if err != nil {
		return nil, err
	}

	var network string
	var host string
	var password string
	var db string

	switch uri.Scheme {
	case "redis":
		network = "tcp"
		host = uri.Host
		if uri.User != nil {
			password, _ = uri.User.Password()
		}
		if len(uri.Path) > 1 {
			db = uri.Path[1:]
		}
	case "unix":
		network = "unix"
		host = uri.Path
	default:
		return nil, errorInvalidScheme
	}

	conn, err := redis.Dial(network, host)
	if err != nil {
		return nil, err
	}

	if password != "" {
		_, err := conn.Do("AUTH", password)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	if db != "" {
		_, err := conn.Do("SELECT", db)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	return &redisConn{Conn: conn}, nil
}
