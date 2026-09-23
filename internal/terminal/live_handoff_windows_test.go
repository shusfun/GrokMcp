//go:build windows

package terminal

import "testing"

// 产品路径真实验收在 supervisor.TestLiveHandoffDuringACPApproval。
// 直接启动第二个 grok --resume 且只比较输出字节数，不能证明模型完整回答。
func TestLiveHandoffDuringACPApproval(t *testing.T) {
	t.Skip("product-path acceptance is supervisor.TestLiveHandoffDuringACPApproval")
}
