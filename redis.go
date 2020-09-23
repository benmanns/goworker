package goworker

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"time"

	"github.com/gomodule/redigo/redis"
	"vitess.io/vitess/go/pools"
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
		return redisConnFromURI(uri)
	}
}

func newRedisPool(uri string, capacity int, maxCapacity int, idleTimout time.Duration) *pools.ResourcePool {
	return pools.NewResourcePool(newRedisFactory(uri), capacity, maxCapacity, idleTimout, 0)
}

func redisConnFromURI(uriString string) (*RedisConn, error) {
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

	conn, err := redis.Dial(network, host, dialOptions...)
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

func getCertPool(certPath string) (*x509.CertPool, error) {
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	certs, err := ioutil.ReadFile(workerSettings.TLSCertPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %q for the RootCA pool: %v", workerSettings.TLSCertPath, err)
	}
	if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
		return nil, fmt.Errorf("Failed to append %q to the RootCA pool: %v", workerSettings.TLSCertPath, err)
	}
	return rootCAs, nil
}
