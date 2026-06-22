package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func peerCredentials(conn *net.UnixConn) (Peer, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var peer Peer
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			controlErr = err
			return
		}
		pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		if err != nil {
			controlErr = err
			return
		}
		peer = Peer{UID: credentials.Uid, GID: credentials.Groups[0], PID: pid}
	}); err != nil {
		return Peer{}, err
	}
	if controlErr != nil {
		return Peer{}, fmt.Errorf("read peer credentials: %w", controlErr)
	}
	return peer, nil
}
