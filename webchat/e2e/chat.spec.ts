import { test, expect, type Page } from '@playwright/test';

// Locators (single source of truth) --------------------------------------

const COMPOSER_PLACEHOLDER = '输入消息，Shift+Enter 换行...';

/** Wait for assistant-ui to hydrate (dynamic import + WS handshake). */
async function waitForChatReady(page: Page) {
  await page.getByPlaceholder(COMPOSER_PLACEHOLDER).waitFor({ state: 'visible', timeout: 20_000 });
}

/** Get the composer textarea. */
function composerInput(page: Page) {
  return page.getByPlaceholder(COMPOSER_PLACEHOLDER);
}

/** Get the send button (inside the composer form, next to textarea). */
function sendButton(page: Page) {
  return page.locator('form button[type="button"]');
}

// Tests ------------------------------------------------------------------

test.describe('Chat Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await waitForChatReady(page);
  });

  test('displays header with correct title and subtitle', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'HotPlex AI' })).toBeVisible();
    await expect(page.getByText(/AEP v1/)).toBeVisible();
  });

  test('shows composer when no messages', async ({ page }) => {
    const input = composerInput(page);
    await expect(input).toBeVisible();
    await expect(input).toBeEditable();
  });

  test('send button becomes enabled when text is entered', async ({ page }) => {
    const sendBtn = sendButton(page);
    await expect(sendBtn).toBeDisabled();

    await composerInput(page).fill('Hello');
    await expect(sendBtn).toBeEnabled();
  });

  test('Enter key submits and clears the input', async ({ page }) => {
    const input = composerInput(page);

    await input.fill('Hello world');
    await input.press('Enter');

    await expect(input).toHaveValue('');
  });

  test('Shift+Enter creates a new line without sending', async ({ page }) => {
    const input = composerInput(page);

    await input.fill('line one');
    await input.press('Shift+Enter');
    await input.pressSequentially('line two');

    const value = await input.inputValue();
    expect(value).toContain('line two');
  });

  test('empty input cannot be sent', async ({ page }) => {
    const sendBtn = sendButton(page);
    await expect(sendBtn).toBeDisabled();

    await composerInput(page).fill('   ');
    await expect(sendBtn).toBeDisabled();
  });

  test('opens and closes the session panel', async ({ page }) => {
    const sessionBtn = page.getByRole('button', { name: /会话/ });
    await expect(sessionBtn).toBeVisible();

    await sessionBtn.click();

    const panel = page.getByRole('dialog', { name: '会话列表' });
    await expect(panel).toBeVisible();

    const closeBtn = panel.getByRole('button', { name: '关闭' });
    await closeBtn.click();

    await expect(panel).not.toBeVisible();
  });
});
