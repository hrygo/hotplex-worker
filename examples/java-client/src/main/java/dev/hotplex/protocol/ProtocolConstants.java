package dev.hotplex.protocol;

/**
 * Protocol constants for AEP v1 communication with HotPlex Gateway.
 */
public final class ProtocolConstants {

    private ProtocolConstants() {
        // Prevent instantiation
    }

    // Protocol version
    public static final String AEP_VERSION = "aep/v1";
    
    // ID prefixes
    public static final String EVENT_ID_PREFIX = "evt_";
    public static final String SESSION_ID_PREFIX = "sess_";

    // Heartbeat defaults (milliseconds)
    public static final long PING_PERIOD_MS = 15000;
    public static final long PONG_WAIT_MS = 5000;
    public static final int MAX_MISSED_PONGS = 3;

    // Reconnect defaults (milliseconds)
    public static final long RECONNECT_BASE_DELAY_MS = 1000;
    public static final long RECONNECT_MAX_DELAY_MS = 60000;
    public static final int RECONNECT_MAX_ATTEMPTS = 10;

    // Session busy retry (milliseconds)
    public static final long SESSION_BUSY_RETRY_DELAY_MS = 2000;

    // Input timeout (milliseconds)
    public static final long INPUT_TIMEOUT_MS = 300000;
}