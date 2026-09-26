package goworker

import (
	"flag"
	"os"
	"strings"
	"time"
)

// Namespace returns the namespace flag for goworker. You
// can use this with the GetConn and PutConn functions to
// operate on the same namespace that goworker uses.
func Namespace() string {
	return workerSettings.Namespace
}

func init() {
	flag.StringVar(&workerSettings.QueuesString, "queues", "", "a comma-separated list of Resque queues")

	flag.Float64Var(&workerSettings.IntervalFloat, "interval", defaultInterval.Seconds(), "sleep interval when no jobs are found")

	flag.IntVar(&workerSettings.Concurrency, "concurrency", defaultConcurrency, "the maximum number of concurrently executing jobs")

	flag.IntVar(&workerSettings.Connections, "connections", defaultConnections, "the maximum number of connections to the Redis database")

	flag.StringVar(&workerSettings.URI, "uri", defaultURI(), "the URI of the Redis server")

	flag.StringVar(&workerSettings.Namespace, "namespace", "resque:", "the Redis namespace")

	flag.StringVar(&workerSettings.TLSCertPath, "tls-cert", "", "path to a custom CA cert")

	flag.BoolVar(&workerSettings.ExitOnComplete, "exit-on-complete", false, "exit when the queue is empty")

	flag.BoolVar(&workerSettings.UseNumber, "use-number", false, "use json.Number instead of float64 when decoding numbers in JSON. will default to true soon")

	flag.BoolVar(&workerSettings.SkipTLSVerify, "insecure-tls", false, "skip TLS validation")
}

const (
	defaultInterval    = 5 * time.Second
	defaultConcurrency = 25
	defaultConnections = 2
)

func defaultURI() string {
	var uri string
	if provider := os.Getenv("REDIS_PROVIDER"); provider != "" {
		uri = os.Getenv(provider)
	} else {
		uri = os.Getenv("REDIS_URL")
	}
	if uri == "" {
		uri = "redis://localhost:6379/"
	}
	return uri
}

func flags() error {
	if !flag.Parsed() {
		flag.Parse()
	}
	// Parse into a fresh value: Set appends, so reusing
	// workerSettings.Queues would duplicate every queue each
	// time Init runs after a Close.
	if workerSettings.QueuesString != "" {
		var queues queuesFlag
		if err := queues.Set(workerSettings.QueuesString); err != nil {
			return err
		}
		workerSettings.Queues = queues
	}
	workerSettings.IsStrict = !strings.ContainsRune(workerSettings.QueuesString, '=')

	// Settings passed to SetSettings start from zero values
	// rather than the flag defaults. Fill in anything that
	// would otherwise leave goworker unable to run: a zero
	// interval polls Redis in a tight loop, zero concurrency
	// never runs a job, and zero connections never gets one.
	if workerSettings.IntervalFloat > 0 {
		if err := workerSettings.Interval.SetFloat(workerSettings.IntervalFloat); err != nil {
			return err
		}
	}
	if time.Duration(workerSettings.Interval) < time.Millisecond {
		workerSettings.Interval = intervalFlag(defaultInterval)
	}
	if workerSettings.Concurrency <= 0 {
		workerSettings.Concurrency = defaultConcurrency
	}
	if workerSettings.Connections <= 0 {
		workerSettings.Connections = defaultConnections
	}
	if workerSettings.URI == "" {
		workerSettings.URI = defaultURI()
	}

	if !workerSettings.UseNumber {
		logger().Warn("deprecation: numbers in job payloads are decoded as float64 and may lose precision; set -use-number to decode them as json.Number and remove this warning")
	}

	return nil
}
