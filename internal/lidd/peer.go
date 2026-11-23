package lidd

// #include <unistd.h>
import "C"

import (
	"errors"
	"net"
)

func peerUID(c net.Conn) (int, error) {
	sc, err := c.(*net.UnixConn).SyscallConn()
	if err != nil {
		return -1, err
	}
	var uid C.uid_t
	var gid C.gid_t
	var rc C.int
	err = sc.Control(func(fd uintptr) { rc = C.getpeereid(C.int(fd), &uid, &gid) })
	if err != nil {
		return -1, err
	}
	if rc != 0 {
		return -1, errors.New("cannot identify socket peer")
	}
	return int(uid), nil
}
