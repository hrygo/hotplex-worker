/** @type {import('next').NextConfig} */

const PREFIX = "HOTPLEX_WEBCHAT_";

// Auto-forward all HOTPLEX_WEBCHAT_* env vars to the client bundle.
// To add a new config: just set it in .env and read from lib/config.ts.
// No changes needed here.
const env = Object.fromEntries(
  Object.entries(process.env)
    .filter(([k]) => k.startsWith(PREFIX))
    .map(([k, v]) => [k, v ?? ""]),
);

const nextConfig = { reactStrictMode: false, output: "export", env };

export default nextConfig;
