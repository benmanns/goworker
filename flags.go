// Running goworker
//
// After building your workers, you will have an
// executable that you can run which will
// automatically poll a Redis server and call
// your workers as jobs arrive.
//
// Flags
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
// the order they are specififed.
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
// pecision. This will default to true soon.
//
// You can also configure your own flags for use
// within your workers. Be sure to set them
// before calling goworker.Main(). It is okay to
// call flags.Parse() before calling
// goworker.Main() if you need to do additional
// processing on your flags.
package goworker

import (
	"flag"
	"os"
	"strings"
)

// Namespace returns the namespace flag for goworker. You
// can use this with the GetConn and PutConn functions to
// operate on the same namespace that goworker uses.
func Namespace() string {
	return workerSettings.Namespace
}

func init() {
	flag.StringVar(&workerSettings.QueuesString, "queues", "", "a comma-separated list of Resque queues")

	flag.Float64Var(&workerSettings.IntervalFloat, "interval", 5.0, "sleep interval when no jobs are found")

	flag.IntVar(&workerSettings.Concurrency, "concurrency", 25, "the maximum number of concurrently executing jobs")

	flag.IntVar(&workerSettings.Connections, "connections", 2, "the maximum number of connections to the Redis database")

	redisProvider := os.Getenv("REDIS_PROVIDER")
	var redisEnvURI string
	if redisProvider != "" {
		redisEnvURI = os.Getenv(redisProvider)
	} else {
		redisEnvURI = os.Getenv("REDIS_URL")
	}
	if redisEnvURI == "" {
		redisEnvURI = "redis://localhost:6379/"
	}
	flag.StringVar(&workerSettings.URI, "uri", redisEnvURI, "the URI of the Redis server")

	flag.StringVar(&workerSettings.Namespace, "namespace", "resque:", "the Redis namespace")

	flag.BoolVar(&workerSettings.ExitOnComplete, "exit-on-complete", false, "exit when the queue is empty")

	flag.BoolVar(&workerSettings.UseNumber, "use-number", false, "use json.Number instead of float64 when decoding numbers in JSON. will default to true soon")
}

func flags() error {
	if !flag.Parsed() {
		flag.Parse()
	}
	if err := workerSettings.Queues.Set(workerSettings.QueuesString); err != nil {
		return err
	}
	if err := workerSettings.Interval.SetFloat(workerSettings.IntervalFloat); err != nil {
		return err
	}
	workerSettings.IsStrict = strings.IndexRune(workerSettings.QueuesString, '=') == -1

	if !workerSettings.UseNumber {
		logger.Warn("== DEPRECATION WARNING ==")
		logger.Warn("  Currently, encoding/json decodes numbers as float64.")
		logger.Warn("  This can cause numbers to lose precision as they are read from the Resque queue.")
		logger.Warn("  Set the -use-number flag to use json.Number when decoding numbers and remove this warning.")
	}

	return nil
}
