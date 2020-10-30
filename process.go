package goworker

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"
)

type process struct {
	Hostname               string
	Pid                    int
	ID                     string
	Queues                 []string
	QueuesPriority         map[string]int
	queuesSortedByPriority []string
}

func newProcess(id string, queues []string, queuesPriority map[string]int) (*process, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	p := &process{
		Hostname:       hostname,
		Pid:            os.Getpid(),
		ID:             id,
		Queues:         queues,
		QueuesPriority: queuesPriority,
	}
	p.queuesSortedByPriority = p.getQueuesSortedByPriority()

	return p, nil
}

func (p *process) String() string {
	return fmt.Sprintf("%s:%d-%s:%s", p.Hostname, p.Pid, p.ID, strings.Join(p.Queues, ","))
}

func (p *process) open(conn *RedisConn) error {
	conn.Send("SADD", fmt.Sprintf("%sworkers", workerSettings.Namespace), p)
	conn.Send("SET", fmt.Sprintf("%sstat:processed:%v", workerSettings.Namespace, p), "0")
	conn.Send("SET", fmt.Sprintf("%sstat:failed:%v", workerSettings.Namespace, p), "0")
	conn.Flush()

	return nil
}

func (p *process) close(conn *RedisConn) error {
	logger.Infof("%v shutdown", p)
	conn.Send("SREM", fmt.Sprintf("%sworkers", workerSettings.Namespace), p)
	conn.Send("DEL", fmt.Sprintf("%sstat:processed:%s", workerSettings.Namespace, p))
	conn.Send("DEL", fmt.Sprintf("%sstat:failed:%s", workerSettings.Namespace, p))
	conn.Flush()

	return nil
}

func (p *process) start(conn *RedisConn) error {
	conn.Send("SET", fmt.Sprintf("%sworker:%s:started", workerSettings.Namespace, p), time.Now().String())
	conn.Flush()

	return nil
}

func (p *process) finish(conn *RedisConn) error {
	conn.Send("DEL", fmt.Sprintf("%sworker:%s", workerSettings.Namespace, p))
	conn.Send("DEL", fmt.Sprintf("%sworker:%s:started", workerSettings.Namespace, p))
	conn.Flush()

	return nil
}

func (p *process) fail(conn *RedisConn) error {
	conn.Send("INCR", fmt.Sprintf("%sstat:failed", workerSettings.Namespace))
	conn.Send("INCR", fmt.Sprintf("%sstat:failed:%s", workerSettings.Namespace, p))
	conn.Flush()

	return nil
}

// queues returns a slice of queues.
// If priorities were provided, this list will be sorted by increasing priorities.
// Priority will be automatically set to zero for those queues without priority.
func (p *process) queues(strict bool) []string {
	if p.QueuesPriority == nil {
		// If the queues order is strict then just return them.
		if strict {
			return p.Queues
		}

		// If not then we want to to shuffle the queues before returning them.
		queues := make([]string, len(p.Queues))
		for i, v := range rand.Perm(len(p.Queues)) {
			queues[i] = p.Queues[v]
		}
		return queues
	}

	return p.queuesSortedByPriority
}

// getQueuesSortedByPriority returns the original queue list sorted by increasing priorities (based on QueuesPriority).
// The original queue list will be unaltered if QueuesPriority is not provided.
func (p *process) getQueuesSortedByPriority() []string {
	// priorities are the same for all queues if no priorities were defined
	if p.QueuesPriority == nil {
		return p.Queues
	}

	// [priority] => []queue
	pQueueMap := make(map[int][]string)

	for _, queue := range p.Queues {
		// get queue's priority
		if priority, ok := p.QueuesPriority[queue]; ok {
			// not allowed a highest priority than zero
			if priority < 0 {
				priority = 0
			}
			// insert queue into priority list
			p.insertQueueIntoPriorityMap(queue, priority, pQueueMap)
		} else {
			// insert queue into priority 0 list
			p.insertQueueIntoPriorityMap(queue, 0, pQueueMap)
		}
	}

	// get the priorities
	priorities := make([]int, 0)
	for k := range pQueueMap {
		priorities = append(priorities, k)
	}
	// sort the priorities in increasing order
	sort.Ints(priorities)

	// generate the queue list: sorted by increasing priorities
	sortedPriorities := make([]string, 0)
	for i := 0; i < len(priorities); i++ {
		sortedPriorities = append(sortedPriorities, pQueueMap[priorities[i]]...)
	}

	return sortedPriorities
}

// insertQueueIntoPriorityMap inserts a queue into a given priority's []string. This function is intended
// to be called ONLY by *process.getQueuesSortedByPriority
func (p *process) insertQueueIntoPriorityMap(queue string, priority int, priorityMap map[int][]string) {
	if qList, ok := priorityMap[priority]; ok {
		priorityMap[priority] = append(qList, queue)
	} else {
		priorityMap[priority] = []string{queue}
	}
}
