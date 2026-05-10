import os
import re

files_weights = {
    "docs/index.md": 1,
    "docs/getting-started.md": 2,
    
    "docs/guides/user/chat-with-ai.md": 1,
    "docs/guides/user/commands-cheatsheet.md": 2,
    "docs/guides/user/mobile-access.md": 3,
    "docs/guides/user/tips-and-tricks.md": 4,

    "docs/guides/developer/remote-coding-agent.md": 11,
    "docs/guides/developer/voice-features.md": 12,
    "docs/guides/developer/cron-automation.md": 13,
    "docs/guides/developer/multiple-agents.md": 14,
    "docs/guides/developer/context-window.md": 15,
    "docs/guides/developer/session-management.md": 16,
    "docs/guides/developer/security-model.md": 17,
    "docs/guides/developer/webchat-setup.md": 18,

    "docs/guides/enterprise/deployment.md": 21,
    "docs/guides/enterprise/security-hardening.md": 22,
    "docs/guides/enterprise/integration-patterns.md": 23,
    "docs/guides/enterprise/multi-tenant.md": 24,
    "docs/guides/enterprise/resource-limits.md": 25,
    "docs/guides/enterprise/observability.md": 26,
    "docs/guides/enterprise/disaster-recovery.md": 27,
    "docs/guides/enterprise/config-management.md": 28,
    "docs/guides/enterprise/compliance.md": 29,

    "docs/guides/contributor/architecture.md": 31,
    "docs/guides/contributor/development-setup.md": 32,
    "docs/guides/contributor/pr-workflow.md": 33,
    "docs/guides/contributor/adding-worker.md": 34,
    "docs/guides/contributor/adding-messaging-adapter.md": 35,
    "docs/guides/contributor/testing-guide.md": 36,
    "docs/guides/contributor/extending.md": 37,

    "docs/tutorials/slack-integration.md": 1,
    "docs/tutorials/feishu-integration.md": 2,
    "docs/tutorials/agent-personality.md": 3,
    "docs/tutorials/cron-scheduled-tasks.md": 4,

    "docs/explanation/why-hotplex.md": 1,
    "docs/explanation/agent-config-system.md": 2,
    "docs/explanation/session-lifecycle.md": 3,
    "docs/explanation/brain-llm-orchestration.md": 4,
    "docs/explanation/cron-design.md": 5,
    "docs/explanation/security-model.md": 6,

    "docs/reference/aep-protocol.md": 1,
    "docs/reference/events.md": 2,
    "docs/reference/configuration.md": 3,
    "docs/reference/cli.md": 4,
    "docs/reference/admin-api.md": 5,
    "docs/reference/sdk-go.md": 6,
    "docs/reference/sdk-python.md": 7,
    "docs/reference/sdk-typescript.md": 8,
    "docs/reference/security-policies.md": 9,
    "docs/reference/glossary.md": 10,
}

for path, weight in files_weights.items():
    if not os.path.exists(path):
        continue
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    if 'weight:' in content:
        content = re.sub(r'weight:\s*\d+', f'weight: {weight}', content)
    else:
        # insert after title: if exists, or at the end of frontmatter
        if 'title:' in content:
            content = re.sub(r'(title:.*?\n)', f'\\g<1>weight: {weight}\n', content, count=1)
        elif content.startswith('---\n'):
            content = re.sub(r'^---\n', f'---\nweight: {weight}\n', content, count=1)
    
    with open(path, 'w', encoding='utf-8') as f:
        f.write(content)

print("Updated weights!")
