package feishu

import "math/rand/v2"

// placeholderIntros are random one-liner HotPlex CLI tips shown in the placeholder card.
var placeholderIntros = []string{
	// 斜杠命令（聊天内）
	"试试 /help 查看所有可用命令",
	"输入 /skills 查看已加载的技能列表",
	"用 /reset 可重置当前会话上下文",
	"斜杠命令可管理会话，无需离开聊天窗口",
	// 常用 CLI
	"hotplex dev 快速启动开发环境",
	"hotplex doctor 检查环境配置是否正确",
	"hotplex update -y --restart 一键更新并重启",
	"hotplex status 查看网关运行状态",
	"hotplex gateway restart -d 以守护进程方式重启网关",
	// Slack 集成
	"hotplex slack send-message 跨平台发送消息",
	"hotplex slack upload-file 快速上传文件到频道",
	"hotplex slack bookmark 管理 Slack 频道书签",
	// 服务管理
	"hotplex service install --level system 安装为系统服务",
	"hotplex service logs -f 实时查看服务日志",
	"hotplex config validate 验证配置文件是否合法",
	// 多平台
	"支持 Slack、飞书、WebChat 多平台同时在线",
}

func randomPlaceholderIntro() string {
	return placeholderIntros[rand.IntN(len(placeholderIntros))]
}
