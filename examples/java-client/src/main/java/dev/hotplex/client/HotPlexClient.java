package dev.hotplex.client;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.hotplex.protocol.*;
import dev.hotplex.security.JwtTokenGenerator;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.web.socket.*;
import org.springframework.web.socket.client.WebSocketClient;
import org.springframework.web.socket.client.standard.StandardWebSocketClient;
import org.springframework.web.socket.handler.TextWebSocketHandler;

import java.io.IOException;
import java.net.URI;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.function.Consumer;

/**
 * HotPlexClient - Java client SDK for HotPlex Gateway (AEP v1).
 *
 * This client provides:
 * - Raw WebSocket connection (NOT STOMP)
 * - Event-driven architecture with listeners
 * - Automatic ping/pong heartbeat
 * - Reconnection with exponential backoff
 * - Async connect using CompletableFuture
 */
public class HotPlexClient extends TextWebSocketHandler {

    private static final Logger log = LoggerFactory.getLogger(HotPlexClient.class);

    // ============================================================================
    // Configuration
    // ============================================================================

    private final String url;
    private final String workerType;
    private final String apiKey;
    private final JwtTokenGenerator tokenGenerator;
    private final InitData.InitConfig config;
    
    private final ObjectMapper objectMapper;
    private WebSocketSession session;
    private final ScheduledExecutorService scheduler;
    
    // ============================================================================
    // State
    // ============================================================================

    private String sessionId;
    private SessionState state = SessionState.Deleted;
    private volatile boolean connected = false;
    private volatile boolean reconnecting = false;
    private volatile boolean closed = false;
    
    private int reconnectAttempt = 0;
    private ScheduledFuture<?> reconnectTimer;
    private ScheduledFuture<?> pingTimer;
    private ScheduledFuture<?> pongTimeoutFuture;
    private int missedPongs = 0;
    private long lastPongTime = 0;
    
    private CompletableFuture<InitAckData> connectFuture;
    
    // ============================================================================
    // Listeners
    // ============================================================================

    private final Map<String, List<Consumer<?>>> listeners = new ConcurrentHashMap<>();

    // ============================================================================
    // Default Configuration
    // ============================================================================

    private static final long PING_INTERVAL_MS = ProtocolConstants.PING_PERIOD_MS;
    private static final long PONG_TIMEOUT_MS = ProtocolConstants.PONG_WAIT_MS;
    private static final int MAX_MISSED_PONGS = ProtocolConstants.MAX_MISSED_PONGS;
    
    private static final long RECONNECT_BASE_DELAY_MS = ProtocolConstants.RECONNECT_BASE_DELAY_MS;
    private static final long RECONNECT_MAX_DELAY_MS = ProtocolConstants.RECONNECT_MAX_DELAY_MS;
    private static final int RECONNECT_MAX_ATTEMPTS = ProtocolConstants.RECONNECT_MAX_ATTEMPTS;

    // ============================================================================
    // Constructor
    // ============================================================================

    /**
     * Create a new HotPlexClient.
     *
     * @param builder the client builder
     */
    private HotPlexClient(Builder builder) {
        this.url = builder.url;
        this.workerType = builder.workerType;
        this.apiKey = builder.apiKey;
        this.tokenGenerator = builder.tokenGenerator;
        this.config = builder.config;
        
        this.objectMapper = new ObjectMapper();
        this.scheduler = Executors.newScheduledThreadPool(2, r -> {
            Thread t = new Thread(r, "hotplex-client-scheduler");
            t.setDaemon(true);
            return t;
        });
    }

    // ============================================================================
    // Public Getters
    // ============================================================================

    public String getSessionId() {
        return sessionId;
    }

    public SessionState getState() {
        return state;
    }

    public boolean isConnected() {
        return connected;
    }

    public boolean isReconnecting() {
        return reconnecting;
    }

    // ============================================================================
    // Connection Lifecycle
    // ============================================================================

    /**
     * Connect to the gateway with a new session.
     *
     * @return CompletableFuture that resolves with InitAckData
     */
    public CompletableFuture<InitAckData> connect() {
        return connect(null);
    }

    /**
     * Connect to the gateway with a specific session ID (for resume).
     *
     * @param existingSessionId the session ID to resume, or null for new session
     * @return CompletableFuture that resolves with InitAckData
     */
    public CompletableFuture<InitAckData> connect(String existingSessionId) {
        if (connected || reconnecting) {
            return CompletableFuture.completedFuture(null);
        }
        
        this.sessionId = existingSessionId != null ? existingSessionId : newSessionId();
        this.connectFuture = new CompletableFuture<>();
        this.shouldReconnect = true;
        
        doConnect();
        
        return connectFuture;
    }

    /**
     * Resume an existing session.
     *
     * @param sessionId the session ID to resume
     * @return CompletableFuture that resolves with InitAckData
     */
    public CompletableFuture<InitAckData> resume(String sessionId) {
        return connect(sessionId);
    }

    private void doConnect() {
        try {
            WebSocketClient client = new StandardWebSocketClient();
            
            WebSocketHttpHeaders wsHeaders = new WebSocketHttpHeaders();
            if (apiKey != null && !apiKey.isEmpty()) {
                wsHeaders.add("X-API-Key", apiKey);
            }
            
            client.execute(this, wsHeaders, URI.create(url));
            
        } catch (Exception e) {
            log.error("Failed to initiate WebSocket connection", e);
            connectFuture.completeExceptionally(e);
            scheduleReconnect();
        }
    }

    /**
     * Internal WebSocket connection handler.
     */
    private void handleConnect() {
        try {
            Envelope initEnvelope = createInitEnvelope();
            String json = objectMapper.writeValueAsString(initEnvelope);
            session.sendMessage(new TextMessage(json + "\n"));
            log.debug("Sent init envelope");
        } catch (Exception e) {
            log.error("Failed to send init envelope", e);
            connectFuture.completeExceptionally(e);
        }
    }

    /**
     * Disconnect from the gateway.
     */
    public void disconnect() {
        closed = true;
        shouldReconnect = false;

        stopHeartbeat();
        clearReconnectTimer();

        if (session != null) {
            try {
                session.close();
            } catch (Exception e) {
                log.debug("Error closing session", e);
            }
            session = null;
        }

        connected = false;
        clearListeners();
        emit("disconnected", "Client initiated disconnect");

        // Shutdown scheduler to release resources
        scheduler.shutdownNow();
    }

    // ============================================================================
    // WebSocket Handler Implementation
    // ============================================================================

    @Override
    public void afterConnectionEstablished(WebSocketSession session) throws Exception {
        log.info("WebSocket connection established");
        this.session = session;
        handleConnect();
    }

    @Override
    protected void handleTextMessage(WebSocketSession session, TextMessage message) throws Exception {
        String payload = message.getPayload();
        if (payload == null || payload.trim().isEmpty()) {
            return;
        }
        
        try {
            Envelope envelope = objectMapper.readValue(payload.trim(), Envelope.class);
            handleEnvelope(envelope);
        } catch (Exception e) {
            log.error("Failed to parse envelope: {}", payload, e);
        }
    }

    @Override
    public void afterConnectionClosed(WebSocketSession session, CloseStatus status) throws Exception {
        log.info("WebSocket connection closed: {} - {}", status.getCode(), status);

        boolean wasConnected = connected;
        connected = false;

        stopHeartbeat();

        if (session != null) {
            session = null;
        }

        if (!wasConnected && !reconnecting) {
            return;
        }

        if (shouldReconnect && !closed) {
            scheduleReconnect();
        } else {
            emit("disconnected", status.toString());
        }
    }

    @Override
    public void handleTransportError(WebSocketSession session, Throwable exception) throws Exception {
        log.error("WebSocket transport error", exception);
        if (session != null) {
            session.close(CloseStatus.SERVER_ERROR);
        }
    }

    // ============================================================================
    // Message Handling
    // ============================================================================

    private void handleEnvelope(Envelope env) {
        String eventType = env.getEvent() != null ? env.getEvent().getType() : null;
        
        if (eventType == null) {
            log.warn("Received envelope without event type");
            return;
        }
        
        // Handle init_ack separately since it completes the connection future
        if ("init_ack".equals(eventType)) {
            handleInitAck(env);
            return;
        }
        
        // Route to appropriate handler
        routeEvent(env);
    }

    private void handleInitAck(Envelope env) {
        try {
            InitAckData ackData = objectMapper.convertValue(env.getEvent().getData(), InitAckData.class);
            
            this.sessionId = ackData.getSessionId() != null ? ackData.getSessionId() : this.sessionId;
            this.state = ackData.getState() != null ? ackData.getState() : SessionState.Created;
            this.connected = true;
            this.reconnecting = false;
            this.reconnectAttempt = 0;
            
            if (reconnectTimer != null) {
                reconnectTimer.cancel(false);
                reconnectTimer = null;
            }
            
            startHeartbeat();
            
            log.info("Connected with sessionId: {}, state: {}", sessionId, state);
            
            emit("connected", ackData);
            connectFuture.complete(ackData);
            
        } catch (Exception e) {
            log.error("Failed to process init_ack", e);
            connectFuture.completeExceptionally(e);
        }
    }

    private void routeEvent(Envelope env) {
        String eventType = env.getEvent().getType();
        Object data = env.getEvent().getData();
        
        switch (eventType) {
            case "error":
                ErrorData errorData = objectMapper.convertValue(data, ErrorData.class);
                emit("error", errorData, env);
                if (errorData.getCode() == ErrorCode.SessionBusy) {
                    handleSessionBusy();
                }
                break;
                
            case "state":
                StateData stateData = objectMapper.convertValue(data, StateData.class);
                this.state = stateData.getState();
                emit("state", stateData, env);
                break;
                
            case "done":
                DoneData doneData = objectMapper.convertValue(data, DoneData.class);
                emit("done", doneData, env);
                break;
                
            case "message.delta":
                MessageDeltaData deltaData = objectMapper.convertValue(data, MessageDeltaData.class);
                emit("messageDelta", deltaData, env);
                break;
                
            case "message":
                MessageData msgData = objectMapper.convertValue(data, MessageData.class);
                emit("message", msgData, env);
                break;
                
            case "message.start":
                Map<?, ?> startData = objectMapper.convertValue(data, Map.class);
                emit("messageStart", startData, env);
                break;
                
            case "message.end":
                Map<?, ?> endData = objectMapper.convertValue(data, Map.class);
                emit("messageEnd", endData, env);
                break;
                
            case "tool_call":
                ToolCallData toolCallData = objectMapper.convertValue(data, ToolCallData.class);
                emit("toolCall", toolCallData, env);
                break;
                
            case "tool_result":
                ToolResultData toolResultData = objectMapper.convertValue(data, ToolResultData.class);
                emit("toolResult", toolResultData, env);
                break;
                
            case "reasoning":
                Map<?, ?> reasoningData = objectMapper.convertValue(data, Map.class);
                emit("reasoning", reasoningData, env);
                break;
                
            case "step":
                Map<?, ?> stepData = objectMapper.convertValue(data, Map.class);
                emit("step", stepData, env);
                break;
                
            case "permission_request":
                PermissionRequestData permData = objectMapper.convertValue(data, PermissionRequestData.class);
                emit("permissionRequest", permData, env);
                break;
                
            case "pong":
                missedPongs = 0;
                lastPongTime = System.currentTimeMillis();
                Map<?, ?> pongData = objectMapper.convertValue(data, Map.class);
                emit("pong", pongData, env);
                break;
                
            case "control":
                handleControlMessage(env);
                break;
                
            case "ping":
                handlePing(env);
                break;
                
            default:
                log.debug("Unhandled event type: {}", eventType);
        }
    }

    private void handleControlMessage(Envelope env) {
        ControlData ctrlData = objectMapper.convertValue(env.getEvent().getData(), ControlData.class);
        
        if (ctrlData == null || ctrlData.getAction() == null) {
            return;
        }
        
        switch (ctrlData.getAction()) {
            case "reconnect":
                emit("reconnect", ctrlData, env);
                if (shouldReconnect) {
                    scheduleReconnect();
                }
                break;
                
            case "session_invalid":
                emit("sessionInvalid", ctrlData, env);
                shouldReconnect = false;
                disconnect();
                break;
                
            case "throttle":
                emit("throttle", ctrlData, env);
                break;
                
            case "terminate":
                emit("terminate", ctrlData, env);
                shouldReconnect = false;
                disconnect();
                break;
                
            case "delete":
                shouldReconnect = false;
                disconnect();
                break;
        }
    }

    private void handlePing(Envelope env) {
        // Send pong response
        try {
            Envelope pongEnvelope = createPongEnvelope();
            String json = objectMapper.writeValueAsString(pongEnvelope);
            if (session != null && session.isOpen()) {
                session.sendMessage(new TextMessage(json + "\n"));
                log.debug("Sent pong response");
            }
        } catch (Exception e) {
            log.error("Failed to send pong", e);
        }
    }

    // ============================================================================
    // Connection Validation
    // ============================================================================

    /**
     * Ensure the client is connected to the gateway.
     *
     * @throws IllegalStateException if not connected
     */
    private void requireConnected() {
        if (!connected || sessionId == null) {
            throw new IllegalStateException("Not connected to gateway");
        }
    }

    // ============================================================================
    // Sending Messages
    // ============================================================================

    /**
     * Send an envelope to the gateway.
     *
     * @param envelope the envelope to send
     * @throws IOException if sending fails
     */
    private void sendEnvelope(Envelope envelope) throws IOException {
        if (session == null || !session.isOpen()) {
            throw new IllegalStateException("WebSocket session not open");
        }
        String json = objectMapper.writeValueAsString(envelope);
        session.sendMessage(new TextMessage(json + "\n"));
    }

    /**
     * Send user input to the worker.
     *
     * @param content the input content
     */
    public void sendInput(String content) {
        sendInput(content, null);
    }

    /**
     * Send user input to the worker with metadata.
     *
     * @param content the input content
     * @param metadata optional metadata
     */
    public void sendInput(String content, Map<String, Object> metadata) {
        requireConnected();

        try {
            InputData inputData = new InputData(content, metadata);
            Envelope envelope = createEnvelope(EventKind.Input.getValue(), inputData, "control");
            sendEnvelope(envelope);
            log.debug("Sent input envelope");
        } catch (Exception e) {
            log.error("Failed to send input", e);
            throw new RuntimeException("Failed to send input", e);
        }
    }

    /**
     * Send input asynchronously and wait for done response.
     *
     * @param content the input content
     * @return CompletableFuture that completes when done is received
     */
    public CompletableFuture<Void> sendInputAsync(String content) {
        return sendInputAsync(content, ProtocolConstants.INPUT_TIMEOUT_MS);
    }

    /**
     * Send input asynchronously with custom timeout.
     *
     * @param content the input content
     * @param timeoutMs timeout in milliseconds
     * @return CompletableFuture that completes when done is received
     */
    public CompletableFuture<Void> sendInputAsync(String content, long timeoutMs) {
        CompletableFuture<Void> future = new CompletableFuture<>();

        // Create listeners array for reference in lambdas
        @SuppressWarnings("unchecked")
        final Consumer<DoneData>[] doneConsumerRef = new Consumer[1];
        @SuppressWarnings("unchecked")
        final Consumer<ErrorData>[] errorConsumerRef = new Consumer[1];

        // Add done listener with self-cleanup
        doneConsumerRef[0] = doneData -> {
            off("done", doneConsumerRef[0]);
            off("error", errorConsumerRef[0]);
            future.complete(null);
        };

        // Add error listener with self-cleanup
        errorConsumerRef[0] = errorData -> {
            off("done", doneConsumerRef[0]);
            off("error", errorConsumerRef[0]);
            future.completeExceptionally(new RuntimeException(errorData.getMessage()));
        };

        on("done", doneConsumerRef[0]);
        on("error", errorConsumerRef[0]);

        // Send the input
        sendInput(content);

        // Timeout handler with cleanup
        scheduler.schedule(() -> {
            off("done", doneConsumerRef[0]);
            off("error", errorConsumerRef[0]);
            if (!future.isDone()) {
                future.completeExceptionally(new TimeoutException("Input timeout"));
            }
        }, timeoutMs, TimeUnit.MILLISECONDS);

        return future;
    }

    /**
     * Send a control message (terminate/delete).
     *
     * @param action the action ("terminate" or "delete")
     */
    public void sendControl(String action) {
        requireConnected();

        try {
            Map<String, String> ctrlData = Map.of("action", action);
            Envelope envelope = createEnvelope(EventKind.Control.getValue(), ctrlData, "control");
            sendEnvelope(envelope);
            log.debug("Sent control envelope: {}", action);
        } catch (Exception e) {
            log.error("Failed to send control", e);
            throw new RuntimeException("Failed to send control", e);
        }
    }

    /**
     * Send a permission response.
     *
     * @param permissionId the permission request ID
     * @param allowed whether the permission is allowed
     * @param reason optional reason
     */
    public void sendPermissionResponse(String permissionId, boolean allowed, String reason) {
        requireConnected();

        try {
            PermissionResponseData permResponse = new PermissionResponseData(permissionId, allowed, reason);
            Envelope envelope = createEnvelope(EventKind.PermissionResponse.getValue(), permResponse, "control");
            sendEnvelope(envelope);
            log.debug("Sent permission response: {} = {}", permissionId, allowed);
        } catch (Exception e) {
            log.error("Failed to send permission response", e);
            throw new RuntimeException("Failed to send permission response", e);
        }
    }

    // ============================================================================
    // Heartbeat
    // ============================================================================

    private void startHeartbeat() {
        stopHeartbeat();
        missedPongs = 0;
        lastPongTime = System.currentTimeMillis();
        
        pingTimer = scheduler.scheduleAtFixedRate(() -> {
            sendPing();
        }, PING_INTERVAL_MS, PING_INTERVAL_MS, TimeUnit.MILLISECONDS);
    }

    private void stopHeartbeat() {
        if (pingTimer != null) {
            pingTimer.cancel(false);
            pingTimer = null;
        }
    }

    private void sendPing() {
        if (session == null || !session.isOpen() || sessionId == null) {
            return;
        }

        try {
            Envelope pingEnvelope = createPingEnvelope();
            String json = objectMapper.writeValueAsString(pingEnvelope);
            session.sendMessage(new TextMessage(json + "\n"));

            // Cancel previous timeout check if still pending
            if (pongTimeoutFuture != null) {
                pongTimeoutFuture.cancel(false);
            }

            // Schedule new timeout check
            pongTimeoutFuture = scheduler.schedule(() -> {
                long timeSinceLastPong = System.currentTimeMillis() - lastPongTime;
                if (timeSinceLastPong >= PONG_TIMEOUT_MS) {
                    missedPongs++;
                    log.warn("Missed pong, count: {}", missedPongs);

                    if (missedPongs >= MAX_MISSED_PONGS) {
                        log.error("Heartbeat timeout");
                        if (session != null) {
                            try {
                                session.close(CloseStatus.GOING_AWAY);
                            } catch (IOException e) {
                                log.debug("Error closing session", e);
                            }
                        }
                    }
                }
            }, PONG_TIMEOUT_MS, TimeUnit.MILLISECONDS);

        } catch (Exception e) {
            log.error("Failed to send ping", e);
        }
    }

    // ============================================================================
    // Reconnection
    // ============================================================================

    private volatile boolean shouldReconnect = true;

    private void scheduleReconnect() {
        if (!shouldReconnect || closed || reconnectAttempt >= RECONNECT_MAX_ATTEMPTS) {
            if (!closed) {
                emit("disconnected", "Max reconnect attempts reached");
            }
            return;
        }
        
        reconnecting = true;
        reconnectAttempt++;
        
        long delay = Math.min(
            RECONNECT_BASE_DELAY_MS * (1L << (reconnectAttempt - 1)),
            RECONNECT_MAX_DELAY_MS
        );
        
        log.info("Scheduling reconnect attempt {} in {} ms", reconnectAttempt, delay);
        emit("reconnecting", reconnectAttempt);
        
        reconnectTimer = scheduler.schedule(() -> {
            if (closed || !shouldReconnect) {
                return;
            }
            doConnect();
        }, delay, TimeUnit.MILLISECONDS);
    }

    private void clearReconnectTimer() {
        if (reconnectTimer != null) {
            reconnectTimer.cancel(false);
            reconnectTimer = null;
        }
    }

    // ============================================================================
    // Session Busy Handling
    // ============================================================================

    private ScheduledFuture<?> sessionBusyRetryTimer;

    private void handleSessionBusy() {
        if (sessionBusyRetryTimer != null) {
            return;
        }
        
        sessionBusyRetryTimer = scheduler.schedule(() -> {
            sessionBusyRetryTimer = null;
            if (session != null && session.isOpen() && pendingInput != null) {
                sendInput(pendingInput);
            }
        }, ProtocolConstants.SESSION_BUSY_RETRY_DELAY_MS, TimeUnit.MILLISECONDS);
    }

    private String pendingInput;

    // ============================================================================
    // Event Listeners
    // ============================================================================

    /**
     * Register an event listener.
     *
     * @param event the event name
     * @param handler the event handler
     */
    public <T> void on(String event, Consumer<T> handler) {
        listeners.computeIfAbsent(event, k -> new CopyOnWriteArrayList<>()).add(handler);
    }

    /**
     * Remove an event listener.
     *
     * @param event the event name
     * @param handler the event handler to remove
     */
    public <T> void off(String event, Consumer<T> handler) {
        List<Consumer<?>> handlers = listeners.get(event);
        if (handlers != null) {
            handlers.remove(handler);
        }
    }

    /**
     * Clear all event listeners.
     */
    public void clearListeners() {
        listeners.clear();
    }

    @SuppressWarnings("unchecked")
    private <T> void emit(String event, T data, Envelope env) {
        List<Consumer<?>> handlers = listeners.get(event);
        if (handlers != null) {
            for (Consumer<?> handler : handlers) {
                try {
                    ((Consumer<T>) handler).accept(data);
                } catch (Exception e) {
                    log.error("Error in event handler for {}", event, e);
                }
            }
        }
    }


    private <T> void emit(String event, T data) {
        emit(event, data, null);
    }

    // ============================================================================
    // Envelope Helpers
    // ============================================================================

    private Envelope createInitEnvelope() {
        InitData initData = new InitData();
        initData.setVersion(ProtocolConstants.AEP_VERSION);
        initData.setWorkerType(workerType);
        if (sessionId != null) {
            initData.setSessionId(sessionId);
        }
        
        if (config != null) {
            initData.setConfig(config);
        }
        
        // Add client capabilities
        InitData.ClientCaps clientCaps = new InitData.ClientCaps();
        clientCaps.setSupportsDelta(true);
        clientCaps.setSupportsToolCall(true);
        clientCaps.setSupportedKinds(List.of(
            "message", "message.delta", "message.start", "message.end",
            "tool_call", "tool_result", "done", "error", "state",
            "reasoning", "step", "control", "ping", "pong"
        ));
        initData.setClientCaps(clientCaps);
        
        // Add auth if token generator is available
        if (tokenGenerator != null) {
            String token = tokenGenerator.generateToken("user", List.of("worker:use"), 3600000);
            InitData.InitAuth auth = new InitData.InitAuth();
            auth.setToken(token);
            initData.setAuth(auth);
        }
        
        return createEnvelope(EventKind.Init.getValue(), initData, "control");
    }

    private Envelope createPingEnvelope() {
        return createEnvelope(EventKind.Ping.getValue(), Map.of(), "control");
    }

    private Envelope createPongEnvelope() {
        return createEnvelope(EventKind.Pong.getValue(), Map.of(), "control");
    }

    private Envelope createEnvelope(String type, Object data, String priority) {
        Envelope envelope = new Envelope();
        envelope.setVersion(ProtocolConstants.AEP_VERSION);
        envelope.setId("evt_" + UUID.randomUUID());
        envelope.setSeq(0L);
        envelope.setSessionId(sessionId);
        envelope.setTimestamp(System.currentTimeMillis());
        envelope.setPriority(priority);
        
        Event event = new Event();
        event.setType(type);
        event.setData(data);
        envelope.setEvent(event);
        
        return envelope;
    }

    private String newSessionId() {
        return "sess_" + UUID.randomUUID();
    }

    // ============================================================================
    // Builder
    // ============================================================================

    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String url;
        private String workerType;
        private String apiKey;
        private JwtTokenGenerator tokenGenerator;
        private InitData.InitConfig config;

        public Builder url(String url) {
            this.url = url;
            return this;
        }

        public Builder workerType(String workerType) {
            this.workerType = workerType;
            return this;
        }

        public Builder apiKey(String apiKey) {
            this.apiKey = apiKey;
            return this;
        }

        public Builder tokenGenerator(JwtTokenGenerator tokenGenerator) {
            this.tokenGenerator = tokenGenerator;
            return this;
        }

        public Builder config(InitData.InitConfig config) {
            this.config = config;
            return this;
        }

        public HotPlexClient build() {
            if (url == null || url.isEmpty()) {
                throw new IllegalArgumentException("url is required");
            }
            if (workerType == null || workerType.isEmpty()) {
                throw new IllegalArgumentException("workerType is required");
            }
            return new HotPlexClient(this);
        }
    }
}