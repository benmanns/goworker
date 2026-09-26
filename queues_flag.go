package goworker

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

var (
	errEmptyQueues      = errors.New("you must specify at least one queue")
	errNonNumericWeight = errors.New("the weight must be a numeric value")
)

type queuesFlag []string

func (q *queuesFlag) Set(value string) error {
	for queueAndWeight := range strings.SplitSeq(value, ",") {
		if queueAndWeight == "" {
			continue
		}

		queue, weight, err := parseQueueAndWeight(queueAndWeight)
		if err != nil {
			return err
		}

		*q = append(*q, slices.Repeat([]string{queue}, max(weight, 0))...)
	}
	if len(*q) == 0 {
		return errEmptyQueues
	}
	return nil
}

func (q *queuesFlag) String() string {
	return fmt.Sprint(*q)
}

func parseQueueAndWeight(queueAndWeight string) (queue string, weight int, err error) {
	queue, weightString, weighted := strings.Cut(queueAndWeight, "=")
	if queue == "" {
		return
	}

	if !weighted {
		weight = 1
	} else {
		weight, err = strconv.Atoi(weightString)
		if err != nil {
			err = errNonNumericWeight
		}
	}
	return
}
