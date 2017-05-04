package goworker

import (
	"errors"
	"net/url"
	"strings"
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
	sentinelConnection   = false
	roleCommandAvailable = true
	roleCommandTested    = false
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
		sentinelConnection = false
		conn, err = redis.Dial(schemeMap[settings.Scheme], settings.Host)
	} else {
		sentinelConnection = true
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
	}

	return pool.Dial()
}

func isSentinelConnection() bool {
	return sentinelConnection
}

func validateConnection(conn *RedisConn) bool {
	if !isSentinelConnection() {
		return true
	}

	// if the instance does not ping back
	_, err := conn.Do("ping")
	if err != nil {
		return false
	}

	if !testRole(conn, "master") {
		return false
	}

	return true
}

func testRole(conn *RedisConn, expectedRole string) bool {
	if !roleCommandTested {
		_, err := conn.Do("ROLE")
		if err != nil {
			roleCommandAvailable = false
		}
		roleCommandTested = true
	}

	if roleCommandAvailable {
		return sentinel.TestRole(conn.Conn, expectedRole)
	} else {
		data, err := conn.Do("INFO")
		if err != nil {
			return false
		}
		str, ok := data.([]uint8)
		if !ok {
			return false
		}
		return strings.Contains(string(str), "role:"+expectedRole)
	}
}
