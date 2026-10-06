package ipc

import (
	"errors"
	"net"
)

func isDisconnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDisconnected) || errors.Is(err, net.ErrClosed) {
		return true
	}
	return isPlatformDisconnect(err)
}

// classifyWrite 按实际写出字节数区分可重试断连和不确定结果。
// 零字节且为断连错误时请求尚未到达对端；一旦写出任何字节，有副作用的调用不能重放。
func classifyWrite(n int, err error) (sent bool, out error) {
	if err == nil {
		return true, nil
	}
	if n == 0 && isDisconnectError(err) {
		if IsDisconnected(err) {
			return false, err
		}
		return false, errors.Join(ErrDisconnected, err)
	}
	if IsUncertain(err) {
		return true, err
	}
	return true, errors.Join(ErrResultUncertain, err)
}
