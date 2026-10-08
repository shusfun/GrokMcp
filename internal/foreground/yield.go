package foreground

import "sync"

// shouldYieldOwnForeground 决定后台进程是否还要处理自己的前台窗口。
// 用户已经要求显示时必须停止，避免把刚打开的窗口交回或藏起来。
func shouldYieldOwnForeground(userShown bool) bool {
	return !userShown
}

// shouldHideOwnForeground 只在后台、且没有其他可见窗口可交回时隐藏自己的前台窗口。
// 找不到目标时隐藏，不能发生在用户已经打开主窗口之后。
func shouldHideOwnForeground(userShown, foundTarget bool) bool {
	return !userShown && !foundTarget
}

var yieldState struct {
	mu        sync.Mutex
	userShown bool
	stop      chan struct{}
}

func markUserShown() {
	yieldState.mu.Lock()
	yieldState.userShown = true
	yieldState.mu.Unlock()
}

func userShown() bool {
	yieldState.mu.Lock()
	defer yieldState.mu.Unlock()
	return yieldState.userShown
}

// StopForUserShow 停止后台交回，并记下用户要看窗口。必须在 Show 之前调用。
func StopForUserShow() {
	markUserShown()
	yieldState.mu.Lock()
	ch := yieldState.stop
	yieldState.stop = nil
	yieldState.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}
