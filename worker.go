package goworker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
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

func (w *worker) start(conn *RedisConn, job *Job) error {
	work := &work{
		Queue:   job.Queue,
		RunAt:   time.Now(),
		Payload: job.Payload,
	}

	buffer, err := json.Marshal(work)
	if err != nil {
		return err
	}

	conn.Send("SET", fmt.Sprintf("%sworker:%s", workerSettings.Namespace, w), buffer)
	logger.Debugf("Processing %s since %s [%v]", work.Queue, work.RunAt, work.Payload.Class)

	return w.process.start(conn)
}

func (w *worker) startHeartbeat() {
	go func() {
		for {
			select {
			case <-w.heartbeatTicker.C:
				conn, err := GetConn()
				if err != nil {
					logger.Criticalf("Error on getting connection in worker %v: %v", w, err)
					return
				}
				conn.Send("HSET", fmt.Sprintf("%s%s", workerSettings.Namespace, heartbeatKey), w.process.String(), time.Now().Format(time.RFC3339))
				PutConn(conn)
			}
		}
	}()
}

func (w *worker) pruneDeadWorkers(conn *RedisConn) {
	// Block with set+nx+ex
	ok, err := conn.Do("SET", fmt.Sprintf("%s%s", workerSettings.Namespace, keyForWorkersPruning), w.String(), "EX", int(heartbeatInterval.Minutes()), "NX")
	if err != nil {
		logger.Criticalf("Error on setting lock to prune workers: %v", err)
		return
	}

	if ok == nil {
		return
	}
	// Get all workers
	iworkers, err := conn.Do("SMEMBERS", fmt.Sprintf("%sworkers", workerSettings.Namespace))
	if err != nil {
		logger.Criticalf("Error on getting list of all workers: %v", err)
		return
	}

	workers := make([]string, 0)
	for _, m := range iworkers.([]interface{}) {
		workers = append(workers, string(m.([]byte)))
	}

	// Get all workers that have sent a heartbeat and now is expired
	iheartbeatWorkers, err := conn.Do("HGETALL", fmt.Sprintf("%s%s", workerSettings.Namespace, heartbeatKey))
	if err != nil {
		logger.Criticalf("Error on getting list of all workers with heartbeat: %v", err)
		return
	}

	hearbeatExpiredWorkers := make(map[string]struct{})
	var currentKey string
	for i, m := range iheartbeatWorkers.([]interface{}) {
		if i%2 == 0 {
			currentKey = string(m.([]byte))
		} else {
			v := string(m.([]byte))
			if v == "" {
				continue
			}

			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				logger.Criticalf("Error on parsing the time of %q: %v", v, err)
				return
			}

			if time.Since(t) > pruneInterval {
				hearbeatExpiredWorkers[currentKey] = struct{}{}
			}
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

			iwork, err := conn.Do("GET", fmt.Sprintf("%sworker:%s", workerSettings.Namespace, wp.String()))
			if err != nil {
				logger.Criticalf("Error on getting worker work for pruning: %v", err)
				return
			}
			if iwork != nil {
				var work = work{}
				err = json.Unmarshal(iwork.([]byte), &work)
				if err != nil {
					logger.Criticalf("Error unmarshaling worker job: %v", err)
					return
				}

				// If it has a job flag it as failed
				wk := worker{process: wp}
				wk.fail(conn, &Job{
					Queue:   work.Queue,
					Payload: work.Payload,
				}, fmt.Errorf("Worker %s did not gracefully exit while processing %s", wk.process.String(), work.Payload.Class))
			}

			wp.close(conn)
		}
	}
}

func (w *worker) fail(conn *RedisConn, job *Job, err error) error {
	failure := &failure{
		FailedAt:  time.Now(),
		Payload:   job.Payload,
		Exception: "Error",
		Error:     err.Error(),
		Worker:    w,
		Queue:     job.Queue,
	}
	buffer, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	conn.Send("RPUSH", fmt.Sprintf("%sfailed", workerSettings.Namespace), buffer)

	return w.process.fail(conn)
}

func (w *worker) succeed(conn *RedisConn, job *Job) error {
	conn.Send("INCR", fmt.Sprintf("%sstat:processed", workerSettings.Namespace))
	conn.Send("INCR", fmt.Sprintf("%sstat:processed:%s", workerSettings.Namespace, w))

	return nil
}

func (w *worker) finish(conn *RedisConn, job *Job, err error) error {
	if err != nil {
		w.fail(conn, job, err)
	} else {
		w.succeed(conn, job)
	}
	return w.process.finish(conn)
}

func (w *worker) work(jobs <-chan *Job, monitor *sync.WaitGroup) {
	conn, err := GetConn()
	if err != nil {
		logger.Criticalf("Error on getting connection in worker %v: %v", w, err)
		return
	}
	w.open(conn)
	PutConn(conn)

	w.startHeartbeat()
	defer w.heartbeatTicker.Stop()

	conn, err = GetConn()
	if err != nil {
		logger.Criticalf("Error on getting connection in worker %v: %v", w, err)
		return
	}
	w.pruneDeadWorkers(conn)
	PutConn(conn)

	monitor.Add(1)

	go func() {
		defer func() {
			defer monitor.Done()

			conn, err := GetConn()
			if err != nil {
				logger.Criticalf("Error on getting connection in worker %v: %v", w, err)
				return
			} else {
				w.close(conn)
				PutConn(conn)
			}
		}()
		for job := range jobs {
			if workerFunc, ok := workers[job.Payload.Class]; ok {
				w.run(job, workerFunc)

				logger.Debugf("done: (Job{%s} | %s | %v)", job.Queue, job.Payload.Class, job.Payload.Args)
			} else {
				errorLog := fmt.Sprintf("No worker for %s in queue %s with args %v", job.Payload.Class, job.Queue, job.Payload.Args)
				logger.Critical(errorLog)

				conn, err := GetConn()
				if err != nil {
					logger.Criticalf("Error on getting connection in worker %v: %v", w, err)
					return
				} else {
					w.finish(conn, job, errors.New(errorLog))
					PutConn(conn)
				}
			}
		}
	}()
}

func (w *worker) run(job *Job, workerFunc workerFunc) {
	var err error
	defer func() {
		conn, errCon := GetConn()
		if errCon != nil {
			logger.Criticalf("Error on getting connection in worker on finish %v: %v", w, errCon)
			return
		} else {
			w.finish(conn, job, err)
			PutConn(conn)
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint(r))
		}
	}()

	conn, err := GetConn()
	if err != nil {
		logger.Criticalf("Error on getting connection in worker on start %v: %v", w, err)
		return
	} else {
		w.start(conn, job)
		PutConn(conn)
	}
	err = workerFunc(job.Queue, job.Payload.Args...)
}
