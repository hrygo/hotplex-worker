import { createHotPlexHandler } from '@/lib/ai-sdk-transport/server/index';

const handleChat = createHotPlexHandler({
  url: process.env.HOTPLEX_WS_URL || 'ws://localhost:8888/ws',
  workerType: process.env.HOTPLEX_WORKER_TYPE || 'claude_code',
  apiKey: process.env.HOTPLEX_API_KEY,
  authToken: process.env.HOTPLEX_AUTH_TOKEN,
});

export async function POST(req: Request) {
  const body = await req.json();
  return handleChat(body);
}
