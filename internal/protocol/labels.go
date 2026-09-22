package protocol

func StageLabel(state JobState) string {
	switch state {
	case StateCreated:
		return "已创建"
	case StateStarting:
		return "启动中"
	case StatePlanning:
		return "规划中"
	case StatePlanReady:
		return "Plan 就绪"
	case StateExecuting:
		return "实施中"
	case StateCompleted:
		return "已完成"
	case StateNeedsInput:
		return "需要输入"
	case StateDisconnected:
		return "待手动恢复"
	case StateRecovering:
		return "恢复中"
	case StateCancelled:
		return "已取消"
	case StateBlocked:
		return "阻塞"
	case StateFailed:
		return "失败"
	default:
		return string(state)
	}
}

func ViewLabel(view ViewMode) string {
	switch view {
	case ViewHeadless:
		return "无头"
	case ViewAttaching:
		return "附着中"
	case ViewHeaded:
		return "有头"
	case ViewDetaching:
		return "分离中"
	default:
		return string(view)
	}
}

func StageViewLabel(state JobState, view ViewMode) string {
	return StageLabel(state) + " · " + ViewLabel(view)
}
