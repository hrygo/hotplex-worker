package feishu

import "math/rand/v2"

// placeholderIntros are random one-liner HotPlex CLI tips shown in the placeholder card.
var placeholderIntros = []string{
	"试试 /help 查看所有可用命令",
	"用 /reset 可重置当前会话上下文",
	"输入 /skills 查看已加载的技能列表",
	"hotplex update 一键更新到最新版本",
	"支持同时连接 Slack、飞书、WebChat 多平台",
	"hotplex service start 后台常驻运行",
	"用斜杠命令管理会话，无需离开聊天窗口",
}

func randomPlaceholderIntro() string {
	return placeholderIntros[rand.IntN(len(placeholderIntros))]
}
