#!/bin/bash
#
# validate-opencode-cli-spec.sh
# 验证 OpenCode CLI Worker 规格文档与实际实现的一致性
#
# Usage: ./validate-opencode-cli-spec.sh

set -euo pipefail

# 需要彩色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SPEC_FILE="$PROJECT_ROOT/docs/specs/Worker-OpenCode-CLI-Spec.md"
OPENCODE_DIR="$HOME/opencode"
OPENCODE_CLI="$OPENCODE_DIR/packages/opencode/src/cli/cmd/run.ts"

echo -e "${BLUE}=== OpenCode CLI Spec 验证工具 ===${NC}"
echo ""

# 检查文件存在性
check_files() {
    echo -e "${BLUE}[1/5] 检查文件存在性${NC}"

    if [[ ! -f "$SPEC_FILE" ]]; then
        echo -e "${RED}✗ Spec 文件不存在: $SPEC_FILE${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Spec 文件存在${NC}"

    if [[ ! -f "$OPENCODE_CLI" ]]; then
        echo -e "${RED}✗ OpenCode CLI 源码不存在: $OPENCODE_CLI${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ OpenCode CLI 源码存在${NC}"
    echo ""
}

# 验证 CLI 参数
validate_cli_args() {
    echo -e "${BLUE}[2/5] 验证 CLI 参数${NC}"

    local spec_args=(
        "run"
        "--format json"
        "--session"
        "--continue"
        "--resume"
        "--allowed-tools"
        "--disallowed-tools"
        "--dangerously-skip-permissions"
        "--permission-mode"
        "--system-prompt"
        "--append-system-prompt"
        "--mcp-config"
        "--strict-mcp-config"
        "--bare"
        "--add-dir"
        "--max-budget-usd"
        "--json-schema"
        "--include-hook-events"
        "--include-partial-messages"
        "--max-turns"
    )

    local found=0
    local not_found=0

    for arg in "${spec_args[@]}"; do
        if grep -q -- "$arg" "$OPENCODE_CLI" 2>/dev/null; then
            echo -e "${GREEN}✓${NC} $arg"
            ((found++))
        else
            echo -e "${YELLOW}?${NC} $arg ${YELLOW}(未在源码中找到)${NC}"
            ((not_found++))
        fi
    done

    echo ""
    echo -e "统计: ${GREEN}$found 个参数已确认${NC}, ${YELLOW}$not_found 个参数待验证${NC}"
    echo ""
}

# 验证环境变量
validate_env_vars() {
    echo -e "${BLUE}[3/5] 验证环境变量${NC}"

    local spec_vars=(
        "OPENAI_API_KEY"
        "OPENAI_BASE_URL"
        "OPENCODE_API_KEY"
        "OPENCODE_BASE_URL"
        "HOTPLEX_SESSION_ID"
        "HOTPLEX_WORKER_TYPE"
    )

    # 在 OpenCode 源码中搜索环境变量
    for var in "${spec_vars[@]}"; do
        if grep -rq "$var" "$OPENCODE_DIR/packages/opencode/src/" 2>/dev/null; then
            echo -e "${GREEN}✓${NC} $var (在源码中找到)"
        else
            echo -e "${YELLOW}?${NC} $var ${YELLOW}(未在源码中找到)${NC}"
        fi
    done

    echo ""
}

# 验证输出格式
validate_output_format() {
    echo -e "${BLUE}[4/5] 验证输出格式${NC}"

    # 检查源码中的 JSON 输出实现
    if grep -q '"json"' "$OPENCODE_CLI" && grep -q 'process.stdout.write.*JSON.stringify' "$OPENCODE_CLI"; then
        echo -e "${GREEN}✓ JSON 格式输出已实现${NC}"
    else
        echo -e "${RED}✗ JSON 格式输出未找到${NC}"
    fi

    # 提取事件类型
    echo ""
    echo -e "${BLUE}源码中定义的事件类型:${NC}"
    grep -E 'emit\("[^"]+"' "$OPENCODE_CLI" | sed 's/.*emit("\([^"]*\)".*/  \1/' | sort -u | while read -r event; do
        echo -e "  ${GREEN}•${NC} $event"
    done

    echo ""
    echo -e "${BLUE}Spec 中定义的事件类型:${NC}"
    grep -A1 '| `.*` | `.*` |' "$SPEC_FILE" | grep -E 'step_start|message|message\.part|tool_use|tool_result|step_end|error|system|session_created' | \
        sed 's/| `\([^`]*\)`.*/  \1/' | while read -r event; do
        echo -e "  ${YELLOW}•${NC} $event"
    done

    echo ""
}

# 生成测试数据
generate_test_data() {
    echo -e "${BLUE}[5/5] 生成测试命令${NC}"

    echo -e "${GREEN}建议的验证命令:${NC}"
    echo ""
    echo -e "  ${YELLOW}1. 基本运行测试:${NC}"
    echo "     cd $OPENCODE_DIR && bun run opencode run --format json 'Hello, world!'"
    echo ""
    echo -e "  ${YELLOW}2. 测试 session 提取:${NC}"
    echo "     cd $OPENCODE_DIR && bun run opencode run --format json 'test' 2>&1 | head -1 | jq -r '.sessionID'"
    echo ""
    echo -e "  ${YELLOW}3. 测试工具调用:${NC}"
    echo "     cd $OPENCODE_DIR && bun run opencode run --format json 'Read package.json'"
    echo ""
    echo -e "  ${YELLOW}4. 测试环境变量:${NC}"
    echo "     HOTPLEX_SESSION_ID=test-123 bun run opencode run --format json 'test'"
    echo ""
}

# 主流程
main() {
    check_files
    validate_cli_args
    validate_env_vars
    validate_output_format
    generate_test_data

    echo -e "${GREEN}=== 验证完成 ===${NC}"
    echo ""
    echo -e "${YELLOW}下一步:${NC}"
    echo "  1. 运行上面的测试命令验证实际输出"
    echo "  2. 对比源码和 Spec 文档的差异"
    echo "  3. 更新 Spec 文档中的实现状态 (✅/⚠️/❌)"
    echo "  4. 记录发现的差异和需要注意的点"
}

main "$@"
