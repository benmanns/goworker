// Running goworker
//
// After building your workers, you will have an
// executable that you can run which will
// automatically poll a Redis server and call
// your workers as jobs arrive.
//
// # Flags
//
// There are several flags which control the
// operation of the goworker client.
//
// -queues="comma,delimited,queues"
// — This is the only required flag. The
// recommended practice is to separate your
// Resque workers from your goworkers with
// different queues. Otherwise, Resque worker
// classes that have no goworker analog will
// cause the goworker process to fail the jobs.
// Because of this, there is no default queue,
// nor is there a way to select all queues (à la
// Resque's * queue). Queues are processed in
// the order they are specified.
// If you have multiple queues you can assign
// them weights. A queue with a weight of 2 will
// be checked twice as often as a queue with a
// weight of 1: -queues='high=2,low=1'.
//
// -interval=5.0
// — Specifies the wait period between polling if
// no job was in the queue the last time one was
// requested.
//
// -concurrency=25
// — Specifies the number of concurrently
// executing workers. This number can be as low
// as 1 or rather comfortably as high as 100,000,
// and should be tuned to your workflow and the
// availability of outside resources.
//
// -connections=2
// — Specifies the maximum number of Redis
// connections that goworker will consume between
// the poller and all workers. There is not much
// performance gain over two and a slight penalty
// when using only one. This is configurable in
// case you need to keep connection counts low
// for cloud Redis providers who limit plans on
// maxclients.
//
// -uri=redis://localhost:6379/
// — Specifies the URI of the Redis database from
// which goworker polls for jobs. Accepts URIs of
// the format redis://user:pass@host:port/db or
// unix:///path/to/redis.sock. The flag may also
// be set by the environment variable
// $($REDIS_PROVIDER) or $REDIS_URL. E.g. set
// $REDIS_PROVIDER to REDISTOGO_URL on Heroku to
// let the Redis To Go add-on configure the Redis
// database.
//
// -namespace=resque:
// — Specifies the namespace from which goworker
// retrieves jobs and stores stats on workers.
//
// -exit-on-complete=false
// — Exits goworker when there are no jobs left
// in the queue. This is helpful in conjunction
// with the time command to benchmark different
// configurations.
//
// -use-number=false
// — Uses json.Number when decoding numbers in the
// job payloads. This will avoid issues that
// occur when goworker and the json package decode
// large numbers as floats, which then get
// encoded in scientific notation, losing
// precision. This will default to true soon.
//
// -tls-cert=""
// — Path to a PEM-encoded CA certificate to trust
// when connecting with a rediss:// URI.
//
// -insecure-tls=false
// — Skips TLS certificate verification for
// rediss:// URIs.
//
// You can also configure your own flags for use
// within your workers. Be sure to set them
// before calling goworker.Work(). It is okay to
// call flag.Parse() before calling
// goworker.Work() if you need to do additional
// processing on your flags.
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
		logger.Warn("deprecation: numbers in job payloads are decoded as float64 and may lose precision; set -use-number to decode them as json.Number and remove this warning")
	}

	return nil
}
