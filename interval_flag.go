package goworker

import (
	"fmt"
	"strconv"
	"time"
)

type intervalFlag time.Duration

func (i *intervalFlag) Set(value string) error {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}
	i.SetFloat(f)
	return nil
}

func (i *intervalFlag) SetFloat(value float64) error {
	*i = intervalFlag(time.Duration(value * float64(time.Second)))
	return nil
}

func (i *intervalFlag) String() string {
	return fmt.Sprint(*i)
}
