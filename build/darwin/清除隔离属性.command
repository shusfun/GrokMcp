#!/bin/sh
set -e

APP="Grok Supervisor.app"
found=""
if [ -d "/Applications/$APP" ]; then
	found="/Applications/$APP"
elif [ -d "$HOME/Applications/$APP" ]; then
	found="$HOME/Applications/$APP"
fi

if [ -z "$found" ]; then
	osascript -e 'display alert "还没有安装 Grok Supervisor" message "请先把应用拖进「应用程序」文件夹，然后再运行这个脚本。" as informational'
	exit 1
fi

xattr -cr "$found"

osascript -e 'display alert "已清除隔离属性" message "现在可以从启动台或「应用程序」打开 Grok Supervisor。系统不会因此关闭 Gatekeeper。" as informational'
open "$found"
