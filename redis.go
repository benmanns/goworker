package goworker

import (
	"errors"
	"github.com/garyburd/redigo/redis"
	"net/url"
	"time"
)

var (
	errorInvalidScheme = errors.New("Invalid Redis database URI scheme.")
)

func newRedisPool(uri string, capacity int, maxCapacity int, idleTimeout time.Duration) *redis.Pool {
	return &redis.Pool {
		MaxIdle: maxCapacity,
		IdleTimeout: idleTimeout,
		Dial: func () (redis.Conn, error) {
			return redisConnFromUri(uri)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

func redisConnFromUri(uriString string) (redis.Conn, error) {
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

	return conn, nil
}
