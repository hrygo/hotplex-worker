package dev.hotplex.example;

import dev.hotplex.client.HotPlexClient;
import dev.hotplex.protocol.*;
import dev.hotplex.security.JwtTokenGenerator;

import java.util.Scanner;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Interactive HotPlex Client Example
 * <p>
 * Demonstrates:
 * - Session management
 * - Interactive chat mode
 * - Event handling
 * <p>
 * Usage:
 * mvn compile && mvn exec:java -Dexec.mainClass="dev.hotplex.example.InteractiveExample"
 * <p>
 * Environment Variables:
 * HOTPLEX_GATEWAY_URL - Gateway URL (default: ws://localhost:8888)
 * HOTPLEX_SIGNING_KEY - JWT signing key (required)
 */
public class InteractiveExample {

    private static final String DEFAULT_GATEWAY_URL = "ws://localhost:8888";

    public static void main(String[] args) throws Exception {
        System.out.println("🚀 HotPlex - Interactive Example\n");
        System.out.println("Commands:");
        System.out.println("  /status  - Show session status");
        System.out.println("  /quit    - Exit\n");

        // Configuration from environment
        String gatewayUrl = getEnvOrDefault("HOTPLEX_GATEWAY_URL", DEFAULT_GATEWAY_URL);
        String signingKey = requireEnv("HOTPLEX_SIGNING_KEY");

        // Create JWT token generator
        JwtTokenGenerator tokenGenerator = new JwtTokenGenerator(signingKey, "hotplex");

        // Create client
        HotPlexClient client = HotPlexClient.builder()
                .url(gatewayUrl)
                .workerType("claude-code")
                .tokenGenerator(tokenGenerator)
                .build();

        // Setup event handlers
        setupEventHandlers(client);

        // Connect
        System.out.println("Connecting to " + gatewayUrl + "...");
        InitAckData ack = client.connect().get(10, TimeUnit.SECONDS);
        System.out.println("✅ Connected!");
        System.out.println("   Session ID: " + ack.getSessionId());
        System.out.println("   State: " + ack.getState() + "\n");

        // Interactive mode
        try (Scanner scanner = new Scanner(System.in)) {
            while (true) {
                System.out.print("\n> ");
                String input = scanner.nextLine().trim();

                if (input.isEmpty()) {
                    continue;
                }

                if (input.startsWith("/")) {
                    // Handle commands
                    String command = input.toLowerCase();

                    if (command.equals("/quit")) {
                        System.out.println("Goodbye!");
                        client.disconnect();
                        return;
                    } else if (command.equals("/status")) {
                        System.out.println("Session ID: " + client.getSessionId());
                        System.out.println("Connected: " + client.isConnected());
                        System.out.println("State: " + client.getState());
                    } else {
                        System.out.println("Unknown command: " + input);
                    }
                } else {
                    // Send as input and wait for response
                    sendAndWait(client, input);
                }
            }
        }
    }

    private static void setupEventHandlers(HotPlexClient client) {
        // Streaming output - print chunks as they arrive
        client.on("messageDelta", (MessageDeltaData data) -> {
            if (data != null && data.getContent() != null) {
                System.out.print(data.getContent());
                System.out.flush();
            }
        });

        // Complete messages
        client.on("message", (MessageData data) -> {
            if (data != null && data.getContent() != null) {
                System.out.println("\n📄 " + data.getContent());
            }
        });

        // State changes
        client.on("state", (StateData data) -> {
            System.out.println("\n📊 State changed to: " + data.getState());
        });

        // Disconnection
        client.on("disconnected", (String reason) -> {
            System.out.println("\n❌ Disconnected: " + reason);
        });
    }

    private static void sendAndWait(HotPlexClient client, String input) {
        CountDownLatch doneLatch = new CountDownLatch(1);

        // Register done handler
        client.on("done", (DoneData data) -> {
            System.out.println("\n\n✅ Task completed!");
            if (data != null && data.getStats() != null) {
                Object duration = data.getStats().get("duration_ms");
                Object tokens = data.getStats().get("total_tokens");
                Object cost = data.getStats().get("cost_usd");

                if (duration != null) {
                    System.out.println("   Duration: " + duration + "ms");
                }
                if (tokens != null) {
                    System.out.println("   Tokens: " + tokens);
                }
                if (cost != null) {
                    System.out.printf("   Cost: $%.4f%n", ((Number) cost).doubleValue());
                }
            }
            doneLatch.countDown();
        });

        // Register error handler
        client.on("error", (ErrorData data) -> {
            System.err.println("\n\n❌ Error: " + data.getCode() + " - " + data.getMessage());
            doneLatch.countDown();
        });

        try {
            // Send input
            client.sendInput(input);

            // Wait for completion (up to 5 minutes)
            if (!doneLatch.await(5, TimeUnit.MINUTES)) {
                System.out.println("\n\n⏱️  Timeout waiting for response");
            }
        } catch (InterruptedException e) {
            System.err.println("\nInterrupted");
            Thread.currentThread().interrupt();
        } catch (Exception e) {
            System.err.println("\nFailed to send input: " + e.getMessage());
        }
    }

    // ============================================================================
    // Utility Methods
    // ============================================================================

    private static String getEnvOrDefault(String name, String defaultValue) {
        String value = System.getenv(name);
        return (value == null || value.isEmpty()) ? defaultValue : value;
    }

    private static String requireEnv(String name) {
        String value = System.getenv(name);
        if (value == null || value.isEmpty()) {
            System.err.println("Error: " + name + " environment variable is required");
            System.err.println("Example: export " + name + "=your-256-bit-secret-key-min-32-chars");
            System.exit(1);
        }
        return value;
    }
}
