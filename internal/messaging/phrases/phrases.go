package phrases

import (
	"math/rand/v2"
	"sort"
)

// entry pairs a phrase text with its source-level weight for weighted random selection.
type entry struct {
	text   string
	weight int
}

// Weight constants by source level: higher = more likely to be selected.
// Fallback (defaults) all have the same weight → uniform random, no bias.
const (
	WeightDefault  = 1 // code defaults (fallback only)
	WeightPlatform = 1 // ~/.hotplex/phrases/{platform}/PHRASES.md
	WeightGlobal   = 2 // ~/.hotplex/phrases/PHRASES.md
	WeightBot      = 4 // ~/.hotplex/phrases/{platform}/{botID}/PHRASES.md
)

// Phrases holds categorized message pools used for procedural UI feedback.
// Immutable after creation — no mutex needed.
type Phrases struct {
	entries map[string][]entry
}

// Random returns a weighted-random entry from the given category.
// Entries from higher-level sources (bot > platform > global > default)
// are proportionally more likely to be selected.
// Returns "" if category not found or empty, or if p is nil.
func (p *Phrases) Random(category string) string {
	if p == nil {
		return ""
	}
	items := p.entries[category]
	if len(items) == 0 {
		return ""
	}
	totalWeight := 0
	for _, e := range items {
		totalWeight += e.weight
	}
	r := rand.IntN(totalWeight)
	for _, e := range items {
		r -= e.weight
		if r < 0 {
			return e.text
		}
	}
	return items[len(items)-1].text
}

// All returns a copy of all entry texts for a category (for preview/debug).
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
	for i, e := range items {
		cp[i] = e.text
	}
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
	return &Phrases{entries: map[string][]entry{
		"greetings": {
			{"来啦～", WeightDefault},
			{"交给我～", WeightDefault},
			{"收到，马上～", WeightDefault},
			{"好嘞！", WeightDefault},
			{"马上来～", WeightDefault},
			{"明白，开始干活！", WeightDefault},
			{"来了来了～", WeightDefault},
			{"收到！", WeightDefault},
		},
		"tips": {
			{"输入 /gc 或 $休眠 可休眠当前会话，下次发消息自动恢复", WeightDefault},
			{"输入 /reset 或 $重置 可重置上下文，从零开始新对话", WeightDefault},
			{"输入 /cd ../other-project 或 $切换目录 ../other-project 切换工作目录", WeightDefault},
			{"输入 ? 或 /help 查看所有可用命令", WeightDefault},
			{"用 $ 前缀可用自然语言触发命令，如 $compact、$上下文、$切换模型", WeightDefault},
			{"输入 /compact 或 $压缩 可压缩历史，释放上下文窗口", WeightDefault},
			{"输入 /commit 或 $提交 可让 AI 快速创建 Git 提交", WeightDefault},
			{"输入 /model sonnet 可切换 AI 模型", WeightDefault},
			{"输入 /context 或 $上下文 可查看上下文窗口使用量", WeightDefault},
			{"输入 /skills 或 $技能 可查看当前已加载的技能列表", WeightDefault},
			{"输入 /mcp 可查看 MCP 服务器连接状态", WeightDefault},
			{"输入 /perm bypassPermissions 可调整权限", WeightDefault},
			{"运行 hotplex onboard 启动交互式配置向导", WeightDefault},
			{"运行 hotplex doctor --fix 可自动检测并修复环境问题", WeightDefault},
			{"运行 hotplex update -y --restart 一键更新并重启 Gateway", WeightDefault},
			{"运行 hotplex dev 可同时启动 Gateway 和 WebChat 开发环境", WeightDefault},
			{"支持 Slack、飞书、WebChat 多平台同时在线", WeightDefault},
		},
		"status": {
			{"Initializing...", WeightDefault},
			{"Thinking...", WeightDefault},
			{"Composing response...", WeightDefault},
			{"Processing...", WeightDefault},
		},
		"welcome": {
			{"Hi，我是 {bot_name}，你的 AI 编程助手！", WeightDefault},
			{"欢迎！直接发消息给我，我们可以开始写代码了。", WeightDefault},
		},
		"welcome_back": {
			{"好久不见！有什么我可以帮你的？", WeightDefault},
			{"欢迎回来～随时继续。", WeightDefault},
		},
		"persona": {
			{"🧠 正在回忆上次对话...", WeightDefault},
			{"📋 加载技能库...", WeightDefault},
			{"🔍 检查工作目录...", WeightDefault},
			{"🎯 分析需求中...", WeightDefault},
			{"🛠️ 准备开发工具...", WeightDefault},
			{"📂 浏览项目结构...", WeightDefault},
			{"💡 思考最佳方案...", WeightDefault},
			{"🚀 引擎预热中...", WeightDefault},
		},
		"closings": {
			{"搞定了！有事随时找我～", WeightDefault},
			{"✅ 完成！还需要什么？", WeightDefault},
			{"搞定～", WeightDefault},
			{"🎉 大功告成！", WeightDefault},
			{"☕ 任务完成，随时待命", WeightDefault},
			{"✨ 处理好了，有事吱声", WeightDefault},
			{"😌 收工～", WeightDefault},
			{"🎯 完美收尾！", WeightDefault},
		},
	}}
}
