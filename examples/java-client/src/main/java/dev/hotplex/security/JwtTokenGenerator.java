package dev.hotplex.security;

import io.jsonwebtoken.JwtBuilder;
import io.jsonwebtoken.Jwts;

import java.math.BigInteger;
import java.security.KeyFactory;
import java.security.KeyPair;
import java.security.PrivateKey;
import java.security.PublicKey;
import java.security.Security;
import java.time.Instant;
import java.util.Date;
import java.util.List;
import java.util.UUID;

import org.bouncycastle.asn1.x9.X9ECParameters;
import org.bouncycastle.jce.provider.BouncyCastleProvider;
import org.bouncycastle.jce.spec.ECParameterSpec;
import org.bouncycastle.math.ec.ECPoint;

/**
 * JWT Token Generator for HotPlex Worker Gateway authentication.
 * Uses ES256 (ECDSA P-256) signing method with key derivation from secret.
 * Key derivation algorithm matches the Go implementation:
 * scalar = (secret_bytes mod (N-1)) + 1, where N is the P-256 curve order.
 */
public class JwtTokenGenerator {

    static {
        Security.addProvider(new BouncyCastleProvider());
    }

    private final String issuer;
    private final String audience;
    private final KeyPair keyPair;

    /**
     * Creates a new JwtTokenGenerator with the specified secret.
     * The secret is used to derive an ECDSA P-256 key pair.
     *
     * @param secret   the secret key (must be at least 32 bytes)
     * @param issuer   the token issuer (e.g., "hotplex")
     */
    public JwtTokenGenerator(String secret, String issuer) {
        this(secret, issuer, "hotplex-gateway");
    }

    /**
     * Creates a new JwtTokenGenerator with the specified secret and audience.
     * The secret is used to derive an ECDSA P-256 key pair.
     *
     * @param secret   the secret key (must be at least 32 bytes)
     * @param issuer   the token issuer (e.g., "hotplex")
     * @param audience the token audience (e.g., "hotplex-gateway")
     */
    public JwtTokenGenerator(String secret, String issuer, String audience) {
        this.issuer = issuer;
        this.audience = audience;
        this.keyPair = deriveKeyPair(secret);
    }

    /**
     * Derives an ECDSA P-256 key pair from the secret.
     * Algorithm matches the Go implementation: scalar mod (N-1) + 1
     */
    private KeyPair deriveKeyPair(String secret) {
        try {
            // Get P-256 curve parameters from BouncyCastle
            X9ECParameters ecParams = org.bouncycastle.asn1.nist.NISTNamedCurves.getByName("P-256");
            org.bouncycastle.math.ec.ECCurve curve = ecParams.getCurve();
            BigInteger n = ecParams.getN();
            BigInteger nMinusOne = n.subtract(BigInteger.ONE);
            ECPoint g = ecParams.getG();

            // Take first 32 bytes of secret
            byte[] secretBytes = secret.getBytes();
            byte[] scalarBytes = new byte[32];
            System.arraycopy(secretBytes, 0, scalarBytes, 0, Math.min(32, secretBytes.length));

            // s = (scalar mod (N-1)) + 1
            BigInteger s = new BigInteger(1, scalarBytes);
            s = s.mod(nMinusOne).add(BigInteger.ONE);

            // Create the private key using BC spec classes
            ECParameterSpec ecSpec = new ECParameterSpec(curve, g, n);
            org.bouncycastle.jce.spec.ECPrivateKeySpec privateKeySpec = 
                new org.bouncycastle.jce.spec.ECPrivateKeySpec(s, ecSpec);
            KeyFactory keyFactory = KeyFactory.getInstance("ECDSA", "BC");
            PrivateKey privateKey = keyFactory.generatePrivate(privateKeySpec);

            // Derive public key: Q = s * G
            ECPoint q = g.multiply(s).normalize();
            org.bouncycastle.jce.spec.ECPublicKeySpec pubKeySpec = 
                new org.bouncycastle.jce.spec.ECPublicKeySpec(q, ecSpec);
            PublicKey publicKey = keyFactory.generatePublic(pubKeySpec);

            return new KeyPair(publicKey, privateKey);
        } catch (Exception e) {
            throw new RuntimeException("Failed to derive ECDSA key pair from secret", e);
        }
    }

    // ============================================================================
    // Private Helper Methods
    // ============================================================================

    /**
     * Creates a base JWT builder with common claims.
     *
     * @param subject the subject (typically user ID)
     * @param scopes  the granted scopes
     * @param ttlSeconds time-to-live in seconds
     * @return the configured JWT builder
     */
    private JwtBuilder baseBuilder(String subject, List<String> scopes, long ttlSeconds) {
        Instant now = Instant.now();
        Instant expiry = now.plusSeconds(ttlSeconds);

        return Jwts.builder()
                .subject(subject)
                .issuer(issuer)
                .audience().add(audience).and()
                .issuedAt(Date.from(now))
                .expiration(Date.from(expiry))
                .claim("scopes", scopes)
                .signWith(keyPair.getPrivate(), Jwts.SIG.ES256);
    }

    // ============================================================================
    // Token Generation Methods
    // ============================================================================

    /**
     * Generates a JWT token with the specified claims.
     *
     * @param subject the subject (typically user ID)
     * @param scopes  the granted scopes
     * @param ttlSeconds time-to-live in seconds
     * @return the signed JWT token string
     */
    public String generateToken(String subject, List<String> scopes, long ttlSeconds) {
        return baseBuilder(subject, scopes, ttlSeconds)
                .id(generateJti())
                .compact();
    }

    /**
     * Generates a JWT token with a specific JTI (JWT ID).
     *
     * @param subject the subject (typically user ID)
     * @param scopes  the granted scopes
     * @param ttlSeconds time-to-live in seconds
     * @param jti     the JWT ID to use
     * @return the signed JWT token string
     */
    public String generateTokenWithJti(String subject, List<String> scopes, long ttlSeconds, String jti) {
        return baseBuilder(subject, scopes, ttlSeconds)
                .id(jti)
                .compact();
    }

    /**
     * Generates a JWT token with custom additional claims.
     *
     * @param subject the subject (typically user ID)
     * @param scopes  the granted scopes
     * @param ttlSeconds time-to-live in seconds
     * @param extraClaims additional claims to include
     * @return the signed JWT token string
     */
    public String generateTokenWithClaims(String subject, List<String> scopes, long ttlSeconds,
                                          java.util.Map<String, Object> extraClaims) {
        JwtBuilder builder = baseBuilder(subject, scopes, ttlSeconds).id(generateJti());

        if (extraClaims != null) {
            extraClaims.forEach(builder::claim);
        }

        return builder.compact();
    }

    /**
     * Gets the key pair for this generator.
     *
     * @return the derived ECDSA key pair
     */
    public KeyPair getKeyPair() {
        return keyPair;
    }

    /**
     * Generates a new JTI (JWT ID) using crypto-safe UUID.
     *
     * @return a unique JWT ID
     */
    public static String generateJti() {
        return UUID.randomUUID().toString();
    }
}
