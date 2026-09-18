import { isWails } from "../api/wails";

export async function confirmPermanentDelete(title: string): Promise<boolean> {
  const message = `永久删除「${title}」？本地任务记录、边界事件和调试 trace 会被删除，Grok session 不会删除。`;
  if (isWails()) {
    const { Dialogs } = await import("@wailsio/runtime");
    const result = await Dialogs.Question({
      Title: "永久删除任务",
      Message: message,
      Buttons: [
        { Label: "取消", IsCancel: true, IsDefault: true },
        { Label: "永久删除" },
      ],
    });
    return result === "永久删除";
  }
  return window.confirm(message);
}
