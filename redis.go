package goworker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

var (
	errInvalidScheme = errors.New("invalid Redis database URI scheme")
)

// RedisConn is a connection from the goworker connection
// pool. See GetConn and PutConn.
type RedisConn struct {
	redis.Conn
}

// Close returns the connection to the pool. It is
// equivalent to PutConn.
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
	var address string
	var dialOptions []redis.DialOption

	switch uri.Scheme {
	case "redis", "rediss":
		network = "tcp"
		address = uri.Host
		if uri.Port() == "" {
			address = net.JoinHostPort(uri.Hostname(), "6379")
		}
		if uri.User != nil {
			if password, ok := uri.User.Password(); ok && password != "" {
				dialOptions = append(dialOptions, redis.DialPassword(password))
			}
		}
		if len(uri.Path) > 1 {
			db, err := strconv.Atoi(uri.Path[1:])
			if err != nil {
				return nil, fmt.Errorf("invalid Redis database %q: %w", uri.Path[1:], err)
			}
			dialOptions = append(dialOptions, redis.DialDatabase(db))
		}
		if uri.Scheme == "rediss" {
			dialOptions = append(dialOptions, redis.DialUseTLS(true))
			config := &tls.Config{
				InsecureSkipVerify: workerSettings.SkipTLSVerify, //nolint:gosec // opt-in via -insecure-tls
			}
			if workerSettings.TLSCertPath != "" {
				pool, err := getCertPool(workerSettings.TLSCertPath)
				if err != nil {
					return nil, err
				}
				config.RootCAs = pool
			}
			dialOptions = append(dialOptions, redis.DialTLSConfig(config))
		}
	case "unix":
		network = "unix"
		address = uri.Path
	default:
		return nil, errInvalidScheme
	}

	return redis.DialContext(ctx, network, address, dialOptions...)
}

func getCertPool(certPath string) (*x509.CertPool, error) {
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	certs, err := os.ReadFile(certPath) //nolint:gosec // the path comes from the -tls-cert setting
	if err != nil {
		return nil, fmt.Errorf("reading %q for the root CA pool: %w", certPath, err)
	}
	if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
		return nil, fmt.Errorf("no PEM certificates found in %q", certPath)
	}
	return rootCAs, nil
}
