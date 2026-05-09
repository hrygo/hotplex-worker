package feishu

import "math/rand/v2"

// placeholderIntros are random one-liner HotPlex CLI tips shown in the placeholder card.
var placeholderIntros = []string{
	// 会话控制
	"输入 /gc 或 $休眠 可暂停当前会话，下次发消息自动恢复",
	"输入 /reset 或 $重置 可清空上下文，从零开始新对话",
	"输入 /cd ../other-project 或 $切换目录 ../other-project 切换工作目录",
	"输入 ? 或 /help 查看所有可用命令",
	"用 $ 前缀可用自然语言触发命令，如 $compact、$上下文、$切换模型",
	// Worker 命令
	"对话过长时输入 /compact 或 $压缩 可压缩历史，释放上下文窗口",
	"输入 /commit 或 $提交 可让 AI 快速创建 Git 提交",
	"输入 /model sonnet 或 $切换模型 sonnet 可切换 AI 模型",
	"输入 /context 或 $上下文 可查看上下文窗口使用量",
	"输入 /skills 或 $技能 可查看当前已加载的技能列表",
	"输入 /mcp 可查看 MCP 服务器连接状态",
	"输入 /perm bypassPermissions 或 $权限模式 bypassPermissions 可调整权限",
	// CLI
	"首次使用？运行 hotplex onboard 启动交互式配置向导",
	"运行 hotplex doctor --fix 可自动检测并修复环境问题",
	"运行 hotplex update -y --restart 一键更新并重启 Gateway",
	"运行 hotplex dev 可同时启动 Gateway 和 WebChat 开发环境",
	// 多平台
	"支持 Slack、飞书、WebChat 多平台同时在线",
}

func randomPlaceholderIntro() string {
	return placeholderIntros[rand.IntN(len(placeholderIntros))]
}
