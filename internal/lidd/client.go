package lidd

import (
	"net"
	"time"

	"agent-coffee/internal/ipc"
)

const lidSocket = "/var/run/agent-coffee-lidd.sock"

func lidRequest(m ipc.Message) (net.Conn, error) {
	c, err := net.DialTimeout("unix", lidSocket, time.Second)
	if err != nil {
		return nil, err
	}
	if err = ipc.Exchange(c, m); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func Check() error {
	c, err := lidRequest(ipc.Message{Op: "check"})
	if err == nil {
		c.Close()
	}
	return err
}

func Acquire(deadline time.Time) (net.Conn, error) {
	return lidRequest(ipc.Message{Op: "acquire", Deadline: deadline})
}

func Drain() error {
	c, err := lidRequest(ipc.Message{Op: "drain"})
	if err == nil {
		c.Close()
	}
	return err
}
