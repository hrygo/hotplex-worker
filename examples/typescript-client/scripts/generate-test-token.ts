import { SignJWT, importJWK } from 'jose';
import * as crypto from 'crypto';

const P256_N = BigInt('0xffffffff00000000ffffffffffffffffbce6faadaa5f1a7f9b7ee7a11e9b4bf65dd65db47c9c52f1ee3a9000000000000ffffffff');
const P256_N_MINUS_1 = P256_N - BigInt(1);

export async function generateTestToken(
  secret: string = 'test-secret-key-for-development-32bytes',
  userId: string = 'test-user',
  issuer: string = 'hotplex',
  ttlSeconds: number = 3600
): Promise<string> {
  const secretBytes = Buffer.from(secret, 'utf-8');
  const d = Buffer.alloc(32);
  d.set(secretBytes.slice(0, 32));
  
  let scalar = BigInt('0x' + d.toString('hex'));
  scalar = (scalar % (P256_N_MINUS_1)) + BigInt(1);
  
  const dHex = scalar.toString(16).padStart(64, '0');
  const dBuf = Buffer.from(dHex, 'hex');
  
  const ecdh = crypto.createECDH('prime256v1');
  ecdh.setPrivateKey(dBuf);
  
  const jwk = {
    kty: 'EC',
    crv: 'P-256',
    x: ecdh.getPublicKey().slice(1, 33).toString('base64url'),
    y: ecdh.getPublicKey().slice(33, 65).toString('base64url'),
    d: dBuf.toString('base64url'),
  };
  
  const privateKey = await importJWK(jwk, 'ES256');
  
  const now = Math.floor(Date.now() / 1000);
  
  return new SignJWT({ user_id: userId, scopes: ['read', 'write'] })
    .setProtectedHeader({ alg: 'ES256', typ: 'JWT' })
    .setIssuer(issuer)
    .setSubject(userId)
    .setAudience(['hotplex-gateway'])
    .setIssuedAt(now)
    .setExpirationTime(now + ttlSeconds)
    .setNotBefore(now)
    .setJti(crypto.randomUUID())
    .sign(privateKey);
}

const args = process.argv.slice(2);
let secret = 'test-secret-key-for-development-32bytes';
let userId = 'test-user';
let issuer = 'hotplex';
let ttl = 3600;

for (let i = 0; i < args.length; i++) {
  if (args[i] === '--secret' && i + 1 < args.length) secret = args[++i];
  else if (args[i] === '--user' && i + 1 < args.length) userId = args[++i];
  else if (args[i] === '--issuer' && i + 1 < args.length) issuer = args[++i];
  else if (args[i] === '--ttl' && i + 1 < args.length) ttl = parseInt(args[++i], 10);
}

generateTestToken(secret, userId, issuer, ttl)
  .then(token => {
    console.log('Generated JWT Token:\n' + token);
    console.log('\nUse in client config: authToken: \'' + token + '\'');
  })
  .catch(err => {
    console.error('Error generating token:', err);
    process.exit(1);
  });