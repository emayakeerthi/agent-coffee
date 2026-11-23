package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"agent-coffee/internal/config"
)

type HookEvent struct {
	Event   string `json:"hook_event_name"`
	Session string `json:"session_id"`
	Turn    string `json:"turn_id"`
}

type Message struct {
	Op       string
	ID       string
	Settings config.Settings
	Event    HookEvent
	Deadline time.Time
}

type reply struct{ Error string }

func Receive(c net.Conn, out any) error {
	c.SetReadDeadline(time.Now().Add(4 * time.Second))
	line, err := bufio.NewReaderSize(c, 16<<10).ReadSlice('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(line, out)
}

func Answer(c net.Conn, err error) error {
	r := reply{}
	if err != nil {
		r.Error = err.Error()
	}
	c.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return json.NewEncoder(c).Encode(r)
}

func Exchange(c net.Conn, m Message) error {
	c.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(c).Encode(m); err != nil {
		return err
	}
	var r reply
	if err := Receive(c, &r); err != nil {
		return err
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

func AwaitClose(c net.Conn) { c.SetReadDeadline(time.Time{}); io.Copy(io.Discard, c) }

func Serve(path string, uid int, fn func(net.Conn)) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	if uid >= 0 {
		if err = os.Chown(path, uid, -1); err != nil {
			return err
		}
	}
	for {
		c, err := listener.Accept()
		if err != nil {
			return err
		}
		go func() { defer c.Close(); fn(c) }()
	}
}
