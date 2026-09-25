package goworker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/gomodule/redigo/redis"
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

func newRedisPool(uri string, capacity int, maxCapacity int, idleTimeout time.Duration) *redis.Pool {
	return &redis.Pool{
		DialContext: func(ctx context.Context) (redis.Conn, error) {
			return redisConnFromURI(ctx, uri)
		},
		MaxIdle:     capacity,
		MaxActive:   maxCapacity,
		IdleTimeout: idleTimeout,
		// Block callers until a connection is available
		// rather than failing, as the previous pool did.
		Wait: true,
		// Connections that sat idle may have been dropped by
		// the server or a proxy; check them before reuse.
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if time.Since(t) < 10*time.Second {
				return nil
			}
			_, err := c.Do("PING")
			return err
		},
	}
}

func redisConnFromURI(ctx context.Context, uriString string) (redis.Conn, error) {
	uri, err := url.Parse(uriString)
	if err != nil {
		return nil, err
	}

	var network string
	var host string
	var password string
	var db string
	var dialOptions []redis.DialOption

	switch uri.Scheme {
	case "redis", "rediss":
		network = "tcp"
		host = uri.Host
		if uri.User != nil {
			password, _ = uri.User.Password()
		}
		if len(uri.Path) > 1 {
			db = uri.Path[1:]
		}
		if uri.Scheme == "rediss" {
			dialOptions = append(dialOptions, redis.DialUseTLS(true))
			dialOptions = append(dialOptions, redis.DialTLSSkipVerify(workerSettings.SkipTLSVerify))
			if len(workerSettings.TLSCertPath) > 0 {
				pool, err := getCertPool(workerSettings.TLSCertPath)
				if err != nil {
					return nil, err
				}
				config := &tls.Config{
					RootCAs: pool,
				}
				dialOptions = append(dialOptions, redis.DialTLSConfig(config))
			}
		}
	case "unix":
		network = "unix"
		host = uri.Path
	default:
		return nil, errorInvalidScheme
	}

	conn, err := redis.DialContext(ctx, network, host, dialOptions...)
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

func getCertPool(certPath string) (*x509.CertPool, error) {
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	certs, err := os.ReadFile(workerSettings.TLSCertPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %q for the RootCA pool: %v", workerSettings.TLSCertPath, err)
	}
	if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
		return nil, fmt.Errorf("Failed to append %q to the RootCA pool: %v", workerSettings.TLSCertPath, err)
	}
	return rootCAs, nil
}
