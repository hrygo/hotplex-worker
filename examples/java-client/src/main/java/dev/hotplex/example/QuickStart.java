package dev.hotplex.example;

import dev.hotplex.client.HotPlexClient;
import dev.hotplex.protocol.*;
import dev.hotplex.security.JwtTokenGenerator;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * HotPlex Gateway - Quick Start Example
 * 
 * Minimal demo showing how to connect to the gateway and send a simple task.
 * 
 * Usage:
 *   mvn compile && mvn exec:java -Dexec.mainClass="dev.hotplex.example.QuickStart"
 * 
 * Environment Variables:
 *   HOTPLEX_GATEWAY_URL - Gateway URL (default: ws://localhost:8888)
 *   HOTPLEX_SIGNING_KEY - JWT signing key (required)
 *   HOTPLEX_TASK        - Task to execute (optional)
 */
public class QuickStart {

    private static final String DEFAULT_GATEWAY_URL = "ws://localhost:8888";
    private static final String DEFAULT_TASK = "Write a hello world program in Go that prints \"Hello, World!\" to stdout.";

    public static void main(String[] args) throws Exception {
        System.out.println("🚀 HotPlex Gateway - Quick Start\n");

        // Configuration from environment
        String gatewayUrl = System.getenv("HOTPLEX_GATEWAY_URL");
        if (gatewayUrl == null || gatewayUrl.isEmpty()) {
            gatewayUrl = DEFAULT_GATEWAY_URL;
        }
        String signingKey = System.getenv("HOTPLEX_SIGNING_KEY");
        if (signingKey == null || signingKey.isEmpty()) {
            System.err.println("Error: HOTPLEX_SIGNING_KEY environment variable is required");
            System.err.println("Example: export HOTPLEX_SIGNING_KEY=your-256-bit-secret-key");
            System.exit(1);
        }
        String task = System.getenv("HOTPLEX_TASK");
        if (task == null || task.isEmpty()) {
            task = DEFAULT_TASK;
        }

        // Create JWT token generator
        JwtTokenGenerator tokenGenerator = new JwtTokenGenerator(signingKey, "hotplex");

        // Create client using builder
        HotPlexClient client = HotPlexClient.builder()
            .url(gatewayUrl)
            .workerType("claude-code")
            .tokenGenerator(tokenGenerator)
            .build();

        // Latch for keeping main thread alive until done
        CountDownLatch doneLatch = new CountDownLatch(1);

        // Set up event listeners
        client.on("messageDelta", (MessageDeltaData data) -> {
            // Streaming output
            if (data != null && data.getContent() != null) {
                System.out.print(data.getContent());
            }
        });

        client.on("done", (DoneData data) -> {
            System.out.println("\n\n✅ Task completed!");
            
            if (data != null && data.getStats() != null) {
                Object duration = data.getStats().get("duration_ms");
                Object totalTokens = data.getStats().get("total_tokens");
                Object cost = data.getStats().get("cost_usd");
                
                System.out.println("   Duration: " + (duration != null ? duration + "ms" : "N/A"));
                System.out.println("   Tokens: " + (totalTokens != null ? totalTokens : "N/A"));
                System.out.println("   Cost: $" + (cost != null ? String.format("%.4f", ((Number) cost).doubleValue()) : "N/A"));
            }
            
            doneLatch.countDown();
        });

        client.on("error", (ErrorData data) -> {
            if (data != null) {
                System.err.println("\n❌ Error: " + data.getCode() + " - " + data.getMessage());
            }
            doneLatch.countDown();
            System.exit(1);
        });

        client.on("disconnected", (String reason) -> {
            System.out.println("Disconnected: " + reason);
        });

        try {
            // Connect to gateway (creates new session)
            System.out.println("Connecting to gateway at: " + gatewayUrl);
            InitAckData ack = client.connect().get();
            System.out.println("Connected! Session: " + ack.getSessionId() + "\n");
            System.out.println("Sending task to Claude Code...\n");

            // Send input task
            client.sendInput(task);

            // Keep main thread alive until done or timeout
            boolean completed = doneLatch.await(5, TimeUnit.MINUTES);
            if (!completed) {
                System.out.println("\nTimeout waiting for task completion");
            }

        } catch (Exception e) {
            System.err.println("Failed to connect: " + e.getMessage());
            System.exit(1);
        } finally {
            client.disconnect();
            System.out.println("Disconnected.");
        }
    }
}