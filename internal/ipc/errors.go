package ipc

import "errors"

// ErrDisconnected 表示请求尚未写到 Supervisor。调用方可以在重新拨号后发送一次。
var ErrDisconnected = errors.New("IPC disconnected")

// ErrResultUncertain 表示请求可能已被 Supervisor 收到，但响应丢失。
// 有副作用的调用不能据此重放。
var ErrResultUncertain = errors.New("IPC 结果不确定：请求可能已经生效。请按 job_id 或 request_id 查询状态，不要重发 dispatch、followup 或审批")

func IsUncertain(err error) bool {
	return errors.Is(err, ErrResultUncertain)
}

func IsDisconnected(err error) bool {
	return errors.Is(err, ErrDisconnected)
}
