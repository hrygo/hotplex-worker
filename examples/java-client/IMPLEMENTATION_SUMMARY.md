# Java Client 快速开始指南

## 编译与运行

```bash
# 1. 设置环境变量
export HOTPLEX_GATEWAY_URL=ws://localhost:8888
export HOTPLEX_SIGNING_KEY=your-256-bit-secret-key-min-32-chars

# 2. 编译项目
cd examples/java-client
mvn clean compile

# 3. 运行 QuickStart 示例
mvn exec:java -Dexec.mainClass="dev.hotplex.example.QuickStart"

# 或者直接运行打包好的 JAR
java -jar target/hotplex-client-1.0.0-SNAPSHOT.jar
```

## 主要功能

### ✅ 已完成
- **完整的客户端 SDK**: HotPlexClient.java (891 行)
  - WebSocket 连接管理
  - 自动心跳检测（ping/pong）
  - 指数退避重连
  - 会话恢复支持
  - 事件监听器架构
  - 异步 API (CompletableFuture)

- **协议层**: 完整的 AEP v1 协议实现
  - 24 个协议类（Envelope, Event, 各种 Data 类型）
  - ProtocolConstants 常量定义
  - ErrorCode 枚举
  - SessionState 枚举

- **安全**: JWT Token 生成器
  - ES256 签名
  - 与 Go 服务器兼容的密钥派生算法

- **示例**: QuickStart.java
  - 最小可用示例
  - 演示连接、发送输入、监听事件

### 📝 文档
- README.md: 完整的 API 文档（已存在）
- logback.xml: 日志配置
- pom.xml: Maven 构建配置

## 依赖版本

```xml
<properties>
    <java.version>17</java.version>
    <jjwt.version>0.12.5</jjwt.version>
</properties>

<dependencies>
    <!-- Spring Boot WebSocket -->
    <dependency>
        <groupId>org.springframework.boot</groupId>
        <artifactId>spring-boot-starter-websocket</artifactId>
    </dependency>

    <!-- Jackson for JSON -->
    <dependency>
        <groupId>com.fasterxml.jackson.core</groupId>
        <artifactId>jackson-databind</artifactId>
    </dependency>

    <!-- JJWT for JWT tokens -->
    <dependency>
        <groupId>io.jsonwebtoken</groupId>
        <artifactId>jjwt-api</artifactId>
        <version>0.12.5</version>
    </dependency>

    <!-- BouncyCastle for ECDSA -->
    <dependency>
        <groupId>org.bouncycastle</groupId>
        <artifactId>bcprov-jdk18on</artifactId>
        <version>1.78</version>
    </dependency>

    <!-- SLF4J + Logback -->
    <dependency>
        <groupId>org.slf4j</groupId>
        <artifactId>slf4j-api</artifactId>
        <version>2.0.12</version>
    </dependency>
    <dependency>
        <groupId>ch.qos.logback</groupId>
        <artifactId>logback-classic</artifactId>
        <version>1.5.3</version>
    </dependency>
</dependencies>
```

## 项目结构

```
examples/java-client/
├── src/main/java/dev/hotplex/
│   ├── client/
│   │   └── HotPlexClient.java          # 主客户端 SDK (891 行)
│   ├── example/
│   │   └── QuickStart.java            # 示例程序 (126 行)
│   ├── protocol/                       # 协议类 (24 个文件)
│   │   ├── Envelope.java
│   │   ├── Event.java
│   │   ├── EventKind.java
│   │   ├── ErrorCode.java
│   │   ├── SessionState.java
│   │   ├── InitData.java
│   │   ├── InitAckData.java
│   │   ├── InputData.java
│   │   ├── MessageData.java
│   │   ├── MessageDeltaData.java
│   │   ├── DoneData.java
│   │   ├── ErrorData.java
│   │   ├── StateData.java
│   │   ├── ToolCallData.java
│   │   ├── ToolResultData.java
│   │   ├── PermissionRequestData.java
│   │   ├── PermissionResponseData.java
│   │   ├── ControlData.java
│   │   ├── PongData.java
│   │   └── ProtocolConstants.java
│   └── security/
│       └── JwtTokenGenerator.java     # JWT 生成器 (191 行)
├── src/main/resources/
│   ├── application.yml              # Spring Boot 配置
│   └── logback.xml                  # 日志配置
├── pom.xml                           # Maven 构建文件
└── README.md                          # 完整文档
```

## 已修复的编译问题

### 问题 1: Lombok 与 Java 17+ 不兼容
**解决方案**: 移除 Lombok，手动使用 SLF4J Logger
```java
// 之前: @Slf4j
// 之后:
private static final Logger log = LoggerFactory.getLogger(HotPlexClient.class);
```

### 问题 2: CloseStatus API 不一致
**解决方案**: 使用 `status.toString()` 替代 `status.getReasonPhrase()`
```java
// 之前: status.getReasonPhrase()
// 之后: status.toString()
```

### 问题 3: 方法名不一致
**解决方案**: 使用 `on/off` 而非 `addListener/removeListener`
```java
// 之前: addListener("done", handler)
// 之后: on("done", handler)
```

### 问题 4: JWT Generator 参数不匹配
**解决方案**: 添加构造函数重载，修复方法名
```java
// 添加了两参数构造函数
public JwtTokenGenerator(String secret, String issuer) {
    this(secret, issuer, "hotplex-gateway");
}

// 修复方法名: generate -> generateToken
public String generateToken(String subject, List<String> scopes, long ttlSeconds)
```

## 构建结果

```bash
$ mvn clean package -DskipTests
[INFO] BUILD SUCCESS
[INFO] Total time:  4.427 s
[INFO] Building jar: target/hotplex-client-1.0.0-SNAPSHOT.jar
```

## 下一步

### 可选改进
1. **单元测试**: 添加 JUnit 测试覆盖核心功能
2. **集成测试**: 针对 running gateway 的端到端测试
3. **文档生成**: 使用 Javadoc 生成 API 文档
4. **Maven Central 发布**: 配置发布到 Maven Central
5. **更多示例**: 添加高级用例示例
   - 流式响应处理
   - 错误恢复
   - 并发会话管理

### 使用建议
1. 在生产环境使用前设置合理的日志级别（logback.xml）
2. 根据实际网络状况调整超时参数（ProtocolConstants）
3. 实现自定义的错误处理和重连策略
4. 为敏感操作添加权限确认机制

## 兼容性

- ✅ Java 17+
- ✅ Spring Boot 3.2.4
- ✅ WebSocket RFC 6455
- ✅ AEP v1 Protocol
- ✅ ES256 JWT (与 Go 服务器兼容)
