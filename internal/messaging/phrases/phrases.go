package phrases

import (
	"math/rand/v2"
	"sort"
)

// Phrases holds categorized message pools used for procedural UI feedback.
// Immutable after creation — no mutex needed.
type Phrases struct {
	entries map[string][]string
}

// Random returns a random entry from the given category.
// Returns "" if category not found or empty, or if p is nil.
func (p *Phrases) Random(category string) string {
	if p == nil {
		return ""
	}
	items := p.entries[category]
	if len(items) == 0 {
		return ""
	}
	return items[rand.IntN(len(items))]
}

// All returns a copy of all entries for a category (for preview/debug).
// Returns nil if category not found.
func (p *Phrases) All(category string) []string {
	if p == nil {
		return nil
	}
	items := p.entries[category]
	if items == nil {
		return nil
	}
	cp := make([]string, len(items))
	copy(cp, items)
	return cp
}

// Categories returns available category names in sorted order.
// Returns nil if p is nil.
func (p *Phrases) Categories() []string {
	if p == nil {
		return nil
	}
	names := make([]string, 0, len(p.entries))
	for k := range p.entries {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Defaults returns the hardcoded base entries migrated from feishu/placeholder.go.
func Defaults() *Phrases {
	return &Phrases{entries: map[string][]string{
		"greetings": {
			"来啦～",
			"交给我～",
			"收到，马上～",
			"好嘞！",
			"马上来～",
			"明白，开始干活！",
			"来了来了～",
			"收到！",
		},
		"tips": {
			// 会话控制
			"输入 /gc 或 $休眠 可休眠当前会话，下次发消息自动恢复",
			"输入 /reset 或 $重置 可重置上下文，从零开始新对话",
			"输入 /cd ../other-project 或 $切换目录 ../other-project 切换工作目录",
			"输入 ? 或 /help 查看所有可用命令",
			"用 $ 前缀可用自然语言触发命令，如 $compact、$上下文、$切换模型",
			// Worker 命令
			"输入 /compact 或 $压缩 可压缩历史，释放上下文窗口",
			"输入 /commit 或 $提交 可让 AI 快速创建 Git 提交",
			"输入 /model sonnet 可切换 AI 模型",
			"输入 /context 或 $上下文 可查看上下文窗口使用量",
			"输入 /skills 或 $技能 可查看当前已加载的技能列表",
			"输入 /mcp 可查看 MCP 服务器连接状态",
			"输入 /perm bypassPermissions 可调整权限",
			// CLI
			"运行 hotplex onboard 启动交互式配置向导",
			"运行 hotplex doctor --fix 可自动检测并修复环境问题",
			"运行 hotplex update -y --restart 一键更新并重启 Gateway",
			"运行 hotplex dev 可同时启动 Gateway 和 WebChat 开发环境",
			// 多平台
			"支持 Slack、飞书、WebChat 多平台同时在线",
		},
		"status": {
			"Initializing...",
			"Thinking...",
			"Composing response...",
			"Processing...",
		},
	}}
}
