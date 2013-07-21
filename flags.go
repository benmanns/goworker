package goworker

import (
	"flag"
)

func init() {
}

func flags() error {
	if !flag.Parsed() {
		flag.Parse()
	}
	return nil
}
