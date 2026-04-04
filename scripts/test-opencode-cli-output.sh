#!/bin/bash
#
# test-opencode-cli-output.sh
# 测试 OpenCode CLI 实际输出格式和事件类型
#
# Usage: ./test-opencode-cli-output.sh [test_case]

set -euo pipefail

OPENCODE_DIR="$HOME/opencode"
OUTPUT_DIR="$(pwd)/test-output"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

mkdir -p "$OUTPUT_DIR"

echo -e "${BLUE}=== OpenCode CLI 输出测试 ===${NC}"
echo ""

# 检查 OpenCode 是否可用
if [ ! -d "$OPENCODE_DIR" ]; then
    echo -e "${RED}错误: OpenCode 目录不存在: $OPENCODE_DIR${NC}"
    exit 1
fi

cd "$OPENCODE_DIR"

# 测试用例 1: 基本文本输出
test_basic_output() {
    echo -e "${YELLOW}[Test 1] 基本文本输出${NC}"
    local output_file="$OUTPUT_DIR/basic_output_${TIMESTAMP}.jsonl"

    echo "运行: bun run opencode run --format json 'Reply with: Hello, World!'"
    timeout 30s bun run opencode run --format json 'Reply with: Hello, World!' 2>&1 | tee "$output_file" || true

    echo ""
    echo -e "${GREEN}输出已保存到: $output_file${NC}"

    # 分析输出
    if [ -s "$output_file" ]; then
        echo ""
        echo -e "${BLUE}=== 输出分析 ===${NC}"

        # 统计事件类型
        echo "事件类型统计:"
        jq -r '.type' "$output_file" 2>/dev/null | sort | uniq -c || echo "  (无法解析 JSON)"

        echo ""
        echo "第一个 step_start 事件:"
        jq 'select(.type == "step_start")' "$output_file" 2>/dev/null | head -1 || echo "  (未找到)"

        echo ""
        echo "第一个 text 事件:"
        jq 'select(.type == "text")' "$output_file" 2>/dev/null | head -1 || echo "  (未找到)"

        echo ""
        echo "Session ID:"
        jq -r '.sessionID' "$output_file" 2>/dev/null | head -1 || echo "  (未找到)"
    else
        echo -e "${RED}错误: 输出文件为空${NC}"
    fi

    echo ""
}

# 测试用例 2: 工具调用
test_tool_usage() {
    echo -e "${YELLOW}[Test 2] 工具调用测试${NC}"
    local output_file="$OUTPUT_DIR/tool_usage_${TIMESTAMP}.jsonl"

    echo "运行: bun run opencode run --format json 'List files in current directory'"
    timeout 30s bun run opencode run --format json 'List files in current directory' 2>&1 | tee "$output_file" || true

    echo ""
    echo -e "${GREEN}输出已保存到: $output_file${NC}"

    if [ -s "$output_file" ]; then
        echo ""
        echo -e "${BLUE}=== 输出分析 ===${NC}"

        echo "工具调用统计:"
        jq -r 'select(.type == "tool_use") | .part.tool' "$output_file" 2>/dev/null | sort | uniq -c || echo "  (无工具调用)"

        echo ""
        echo "第一个 tool_use 事件:"
        jq 'select(.type == "tool_use")' "$output_file" 2>/dev/null | head -1 || echo "  (未找到)"
    else
        echo -e "${RED}错误: 输出文件为空${NC}"
    fi

    echo ""
}

# 测试用例 3: 错误处理
test_error_handling() {
    echo -e "${YELLOW}[Test 3] 错误处理测试${NC}"
    local output_file="$OUTPUT_DIR/error_handling_${TIMESTAMP}.jsonl"

    echo "运行: bun run opencode run --format json 'Read file /nonexistent/path'"
    timeout 30s bun run opencode run --format json 'Read file /nonexistent/path' 2>&1 | tee "$output_file" || true

    echo ""
    echo -e "${GREEN}输出已保存到: $output_file${NC}"

    if [ -s "$output_file" ]; then
        echo ""
        echo -e "${BLUE}=== 输出分析 ===${NC}"

        echo "错误事件:"
        jq 'select(.type == "error")' "$output_file" 2>/dev/null || echo "  (无错误)"
    else
        echo -e "${RED}错误: 输出文件为空${NC}"
    fi

    echo ""
}

# 测试用例 4: Session 管理
test_session_management() {
    echo -e "${YELLOW}[Test 4] Session 管理测试${NC}"
    local output_file="$OUTPUT_DIR/session_test_${TIMESTAMP}.jsonl"

    echo "运行: bun run opencode run --format json 'test'"
    timeout 30s bun run opencode run --format json 'test' 2>&1 | tee "$output_file" || true

    echo ""
    echo -e "${GREEN}输出已保存到: $output_file${NC}"

    if [ -s "$output_file" ]; then
        echo ""
        echo -e "${BLUE}=== 输出分析 ===${NC}"

        echo "Session ID 提取:"
        local session_id=$(jq -r '.sessionID' "$output_file" 2>/dev/null | head -1)
        if [ -n "$session_id" ] && [ "$session_id" != "null" ]; then
            echo "  Session ID: $session_id"
            echo "  长度: ${#session_id}"
            echo "  格式: $(echo "$session_id" | grep -E '^(sess_|session_|)[a-f0-9-]+$' && echo 'UUID-like' || echo 'Unknown')"
        else
            echo "  (未找到 Session ID)"
        fi

        echo ""
        echo "step_start 事件中的 session_id:"
        jq 'select(.type == "step_start") | .part.sessionID' "$output_file" 2>/dev/null | head -1 || echo "  (未找到)"
    else
        echo -e "${RED}错误: 输出文件为空${NC}"
    fi

    echo ""
}

# 测试用例 5: 环境变量注入
test_env_injection() {
    echo -e "${YELLOW}[Test 5] 环境变量注入测试${NC}"
    local output_file="$OUTPUT_DIR/env_test_${TIMESTAMP}.jsonl"

    echo "运行: HOTPLEX_SESSION_ID=test-session-123 bun run opencode run --format json 'test'"
    timeout 30s HOTPLEX_SESSION_ID=test-session-123 bun run opencode run --format json 'test' 2>&1 | tee "$output_file" || true

    echo ""
    echo -e "${GREEN}输出已保存到: $output_file${NC}"

    if [ -s "$output_file" ]; then
        echo ""
        echo -e "${BLUE}=== 输出分析 ===${NC}"

        echo "检查环境变量是否影响 session_id:"
        jq -r '.sessionID' "$output_file" 2>/dev/null | head -1 || echo "  (未找到)"
    else
        echo -e "${RED}错误: 输出文件为空${NC}"
    fi

    echo ""
}

# 测试用例 6: 格式对比
test_format_comparison() {
    echo -e "${YELLOW}[Test 6] JSON vs 默认格式对比${NC}"
    local json_output="$OUTPUT_DIR/format_json_${TIMESTAMP}.jsonl"
    local default_output="$OUTPUT_DIR/format_default_${TIMESTAMP}.txt"

    echo "运行 [JSON 格式]: bun run opencode run --format json 'Say hello'"
    timeout 30s bun run opencode run --format json 'Say hello' 2>&1 | tee "$json_output" || true

    echo ""
    echo "运行 [默认格式]: bun run opencode run 'Say hello'"
    timeout 30s bun run opencode run 'Say hello' 2>&1 | tee "$default_output" || true

    echo ""
    echo -e "${GREEN}JSON 输出: $json_output${NC}"
    echo -e "${GREEN}默认输出: $default_output${NC}"

    echo ""
    echo -e "${BLUE}=== 格式对比 ===${NC}"
    echo "JSON 输出行数: $(wc -l < "$json_output")"
    echo "默认输出行数: $(wc -l < "$default_output")"

    echo ""
}

# 主流程
main() {
    local test_case="${1:-all}"

    case "$test_case" in
        1|basic)
            test_basic_output
            ;;
        2|tool)
            test_tool_usage
            ;;
        3|error)
            test_error_handling
            ;;
        4|session)
            test_session_management
            ;;
        5|env)
            test_env_injection
            ;;
        6|format)
            test_format_comparison
            ;;
        all)
            test_basic_output
            test_tool_usage
            test_error_handling
            test_session_management
            test_env_injection
            test_format_comparison
            ;;
        *)
            echo -e "${RED}未知测试用例: $test_case${NC}"
            echo "可用测试: 1|basic, 2|tool, 3|error, 4|session, 5|env, 6|format, all"
            exit 1
            ;;
    esac

    echo -e "${GREEN}=== 测试完成 ===${NC}"
    echo ""
    echo "所有输出文件保存在: $OUTPUT_DIR"
    echo ""
    echo "下一步:"
    echo "  1. 检查输出文件: ls -lh $OUTPUT_DIR"
    echo "  2. 分析单个文件: jq '.' $OUTPUT_DIR/basic_output_*.jsonl | head -50"
    echo "  3. 对比 Spec: diff <(jq '.' file.jsonl) <(cat expected.json)"
}

main "$@"
