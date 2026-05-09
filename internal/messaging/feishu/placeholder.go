package feishu

import "math/rand/v2"

// placeholderIntros are random one-liner HotPlex CLI tips shown in the placeholder card.
var placeholderIntros = []string{
	// 会话控制
	"试试 /gc 或 $休眠 暂停当前会话",
	"/reset 或 $重置 可清空上下文重新开始",
	"输入 ? 或 /help 查看所有可用命令",
	"$前缀可用自然语言触发命令，如 $上下文",
	"用 /cd <目录> 或 $切换目录 切换工作路径",
	// Worker 命令
	"/compact 或 $压缩 可压缩对话历史",
	"/rewind 或 $回退 可撤销上一轮对话",
	"/commit 或 $提交 快速创建 Git 提交",
	"/model <名称> 或 $切换模型 切换 AI 模型",
	"输入 /context 或 $上下文 查看上下文窗口用量",
	"输入 /skills 或 $技能 查看已加载的技能列表",
	// CLI
	"hotplex onboard 交互式配置向导",
	"hotplex doctor --fix 自动修复环境问题",
	"hotplex update -y --restart 一键更新并重启",
	"hotplex dev 快速启动开发环境",
	"hotplex config validate 验证配置文件是否合法",
	// 多平台
	"支持 Slack、飞书、WebChat 多平台同时在线",
}

func randomPlaceholderIntro() string {
	return placeholderIntros[rand.IntN(len(placeholderIntros))]
}
