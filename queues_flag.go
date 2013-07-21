package goworker

import (
	"errors"
	"fmt"
	"strings"
)

var (
	errorEmptyQueues = errors.New("You must specify at least one queue.")
)

type queuesFlag []string

func (q *queuesFlag) Set(value string) error {
	if value == "" {
		return errorEmptyQueues
	}

	*q = append(*q, strings.Split(value, ",")...)
	return nil
}

func (q *queuesFlag) String() string {
	return fmt.Sprint(*q)
}
