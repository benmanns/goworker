package goworker

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/pkg/errors"
)

const (
	heartbeatInterval    = time.Minute
	heartbeatKey         = "workers:heartbeat"
	keyForWorkersPruning = "pruning_dead_workers_in_progress"
	pruneInterval        = heartbeatInterval * 5
)

type worker struct {
	process
	heartbeatTicker *time.Ticker
}

func newWorker(id string, queues []string) (*worker, error) {
	process, err := newProcess(id, queues)
	if err != nil {
		return nil, err
	}
	return &worker{
		process:         *process,
		heartbeatTicker: time.NewTicker(heartbeatInterval),
	}, nil
}

func (w *worker) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.String())
}

func (w *worker) start(c *redis.Client, job *Job) error {
	work := &work{
		Queue:   job.Queue,
		RunAt:   time.Now(),
		Payload: job.Payload,
	}

	buffer, err := json.Marshal(work)
	if err != nil {
		return err
	}

	err = c.Set(c.Context(), fmt.Sprintf("%sworker:%s", workerSettings.Namespace, w), buffer, 0).Err()
	if err != nil {
		return err
	}

	logger.Debugf("Processing %s since %s [%v]", work.Queue, work.RunAt, work.Payload.Class)

	return w.process.start(c)
}

func (w *worker) startHeartbeat(c *redis.Client) {
	go func() {
		for {
			select {
			case <-w.heartbeatTicker.C:
				err := c.HSet(c.Context(), fmt.Sprintf("%s%s", workerSettings.Namespace, heartbeatKey), w.process.String(), time.Now().Format(time.RFC3339)).Err()
				if err != nil {
					_ = logger.Errorf("Error on worker heartbeat %v: %v", w, err)
				}
			}
		}
	}()
}

func (w *worker) pruneDeadWorkers(c *redis.Client) {
	// Block with set+nx+ex
	ok, err := c.SetNX(c.Context(), fmt.Sprintf("%s%s", workerSettings.Namespace, keyForWorkersPruning), w.String(), heartbeatInterval.Truncate(time.Second)).Result()
	if err != nil {
		_ = logger.Criticalf("Error on setting lock to prune workers: %v", err)
		return
	}

	if ok == false {
		return
	}
	// Get all workers
	workers, err := c.SMembers(c.Context(), fmt.Sprintf("%sworkers", workerSettings.Namespace)).Result()
	if err != nil {
		_ = logger.Criticalf("Error on getting list of all workers: %v", err)
		return
	}

	// Get all workers that have sent a heartbeat and now is expired
	heartbeatWorkers, err := c.HGetAll(c.Context(), fmt.Sprintf("%s%s", workerSettings.Namespace, heartbeatKey)).Result()
	if err != nil {
		_ = logger.Criticalf("Error on getting list of all workers with heartbeat: %v", err)
		return
	}

	hearbeatExpiredWorkers := make(map[string]struct{})
	for k, v := range heartbeatWorkers {
		if v == "" {
			continue
		}

		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			_ = logger.Criticalf("Error on parsing the time of %q: %v", v, err)
			return
		}

		if time.Since(t) > pruneInterval {
			hearbeatExpiredWorkers[k] = struct{}{}
		}
	}

	// If a worker is on the expired list kill it
	for _, w := range workers {
		if _, ok := hearbeatExpiredWorkers[w]; ok {
			logger.Infof("Pruning dead worker %q", w)

			parts := strings.Split(w, ":")
			pidAndID := strings.Split(parts[1], "-")
			pid, _ := strconv.Atoi(pidAndID[0])
			wp := process{
				Hostname: parts[0],
				Pid:      int(pid),
				ID:       pidAndID[1],
				Queues:   strings.Split(parts[2], ","),
			}

			works, err := c.Get(c.Context(), fmt.Sprintf("%sworker:%s", workerSettings.Namespace, wp.String())).Result()
			if err != nil {
				_ = logger.Criticalf("Error on getting worker work for pruning: %v", err)
				return
			}
			if works != "" {
				var work = work{}
				err = json.Unmarshal([]byte(works), &work)
				if err != nil {
					_ = logger.Criticalf("Error unmarshaling worker job: %v", err)
					return
				}

				// If it has a job flag it as failed
				wk := worker{process: wp}
				err = wk.fail(c, &Job{
					Queue:   work.Queue,
					Payload: work.Payload,
				}, fmt.Errorf("worker %s did not gracefully exit while processing %s", wk.process.String(), work.Payload.Class))
				if err != nil {
					_ = logger.Criticalf("Error failing worker job: %v", err)
					return
				}
			}

			err = wp.close(c)
			if err != nil {
				_ = logger.Criticalf("Error closing connection to Redis: %v", err)
				return
			}
		}
	}
}

func (w *worker) fail(c *redis.Client, job *Job, err error) error {
	failure := &failure{
		FailedAt:  time.Now(),
		Payload:   job.Payload,
		Exception: "Error",
		// %+v for errors with stack produces stack with file and method names and line numbers
		Error:  fmt.Sprintf("%+v", err),
		Worker: w,
		Queue:  job.Queue,
	}
	buffer, err := json.Marshal(failure)
	if err != nil {
		return err
	}

	err = c.RPush(c.Context(), fmt.Sprintf("%sfailed", workerSettings.Namespace), buffer).Err()
	if err != nil {
		return err
	}

	return w.process.fail(c)
}

func (w *worker) succeed(c *redis.Client) error {
	err := c.Incr(c.Context(), fmt.Sprintf("%sstat:processed", workerSettings.Namespace)).Err()
	if err != nil {
		return err
	}

	err = c.Incr(c.Context(), fmt.Sprintf("%sstat:processed:%s", workerSettings.Namespace, w)).Err()
	if err != nil {
		return err
	}

	return nil
}

func (w *worker) finish(c *redis.Client, job *Job, err error) error {
	if err != nil {
		err = w.fail(c, job, errors.WithStack(err))
		if err != nil {
			return errors.WithStack(err)
		}
	} else {
		err = w.succeed(c)
		if err != nil {
			return err
		}
	}
	return w.process.finish(c)
}

func (w *worker) work(jobs <-chan *Job, monitor *sync.WaitGroup) {
	err := w.open(client)
	if err != nil {
		_ = logger.Criticalf("Error on opening worker %v: %v", w, err)
		return
	}

	w.startHeartbeat(client)
	defer w.heartbeatTicker.Stop()

	w.pruneDeadWorkers(client)

	monitor.Add(1)

	go func() {
		defer func() {
			defer monitor.Done()

			err := w.close(client)
			if err != nil {
				_ = logger.Criticalf("Error on closing worker %v: %v", w, err)
				return
			}
		}()
		for job := range jobs {
			if workerFunc, ok := workers[job.Payload.Class]; ok {
				w.run(job, workerFunc)

				logger.Debugf("done: (Job{%s} | %s | %v)", job.Queue, job.Payload.Class, job.Payload.Args)
			} else {
				errorLog := fmt.Sprintf("No worker for %s in queue %s with args %v", job.Payload.Class, job.Queue, job.Payload.Args)
				_ = logger.Critical(errorLog)

				err := w.finish(client, job, errors.New(errorLog))
				if err != nil {
					_ = logger.Criticalf("Error on finishing worker %v: %v", w, err)
					return
				}
			}
		}
	}()
}

func (w *worker) run(job *Job, workerFunc workerFunc) {
	var err error

	defer func() {
		errFinish := w.finish(client, job, err)
		if errFinish != nil {
			_ = logger.Criticalf("Error on finishing worker %v: %v", w, errFinish)
			return
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint(r))
		}
	}()

	errStart := w.start(client, job)
	if errStart != nil {
		_ = logger.Criticalf("Error on starting worker %v: %v", w, errStart)
		return
	}

	err = workerFunc(job.Queue, job.Payload.Args...)
}
