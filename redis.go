package goworker

import (
	"errors"
	"net/url"
	"time"

	"github.com/FZambia/go-sentinel"
	"github.com/garyburd/redigo/redis"
	"github.com/youtube/vitess/go/pools"
)

var (
	errorInvalidScheme = errors.New("invalid Redis database URI scheme")
	schemeMap          = map[string]string{
		"":      "tcp",
		"redis": "tcp",
		"unix":  "unix",
	}
)

type RedisConn struct {
	redis.Conn
}

func (r *RedisConn) Close() {
	_ = r.Conn.Close()
}

func newRedisPool(settings RedisSettings, capacity int, maxCapacity int, idleTimout time.Duration) *pools.ResourcePool {
	return pools.NewResourcePool(newRedisFactory(settings), capacity, maxCapacity, idleTimout)
}

func newRedisFactory(settings RedisSettings) pools.Factory {
	return func() (pools.Resource, error) {
		return redisConnFromSettings(settings)
	}
}

func parseSettingsURI(settings *RedisSettings) error {
	uri, err := url.Parse(settings.URI)
	if err != nil {
		return err
	}
	settings.Scheme = uri.Scheme

	switch settings.Scheme {
	case "redis":
		settings.Host = uri.Host
		if uri.User != nil {
			settings.Password, _ = uri.User.Password()
		}
		if len(uri.Path) > 1 {
			settings.DB = uri.Path[1:]
		}
	case "unix":
		settings.Host = uri.Path
	default:
		return errorInvalidScheme
	}
	return nil
}

func redisConnFromSettings(settings RedisSettings) (*RedisConn, error) {
	var err error
	var conn redis.Conn
	if settings.URI != "" {
		err = parseSettingsURI(&settings)
		if err != nil {
			return nil, err
		}
	}

	if settings.MasterName == "" {
		conn, err = redis.Dial(schemeMap[settings.Scheme], settings.Host)
	} else {
		conn, err = redisSentinelConnection(settings)
	}
	if err != nil {
		return nil, err
	}

	if settings.Password != "" {
		_, err := conn.Do("AUTH", settings.Password)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	if settings.DB != "" {
		_, err := conn.Do("SELECT", settings.DB)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	return &RedisConn{Conn: conn}, nil
}

func redisSentinelConnection(settings RedisSettings) (redis.Conn, error) {
	sntnl := &sentinel.Sentinel{
		Addrs:      settings.Sentinels,
		MasterName: settings.MasterName,
		Dial: func(addr string) (redis.Conn, error) {
			timeout := settings.Timeout
			return redis.DialTimeout(schemeMap[settings.Scheme], addr, timeout, timeout, timeout)
		},
	}
	pool := &redis.Pool{
		Wait:        true,
		IdleTimeout: time.Second,
		Dial: func() (redis.Conn, error) {

			masterAddr, err := sntnl.MasterAddr()
			if err != nil {
				return nil, err
			}
			return redis.Dial(schemeMap[settings.Scheme], masterAddr)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if !sentinel.TestRole(c, "master") {
				return errors.New("Role check failed")
			} else {
				return nil
			}
		},
	}

	return pool.Dial()
}
