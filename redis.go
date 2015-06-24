package goworker

import (
	"errors"
	"github.com/garyburd/redigo/redis"
	"github.com/youtube/vitess/go/pools"
	"net/url"
	"time"
)

var (
	errorInvalidScheme = errors.New("invalid Redis database URI scheme")
)

type RedisConn struct {
	redis.Conn
}

func (r *RedisConn) Close() {
	_ = r.Conn.Close()
}

func newRedisFactory(uri string) pools.Factory {
	return func() (pools.Resource, error) {
		return redisConnFromUri(uri)
	}
}

func newRedisPool(uri string, capacity int, maxCapacity int, idleTimout time.Duration) *pools.ResourcePool {
	return pools.NewResourcePool(newRedisFactory(uri), capacity, maxCapacity, idleTimout)
}

func redisConnFromUri(uriString string) (*RedisConn, error) {
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

	return &RedisConn{Conn: conn}, nil
}
