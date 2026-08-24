#!/usr/bin/env node
// T-260 (FR-81 / N10): minimal OIDC IdP for the armed-instance e2e leg of
// e2e/m9/oidc-stepup.spec.ts.
//
// The T-224 QA round proved the server contract against a real Keycloak
// container; the console spec needs a *repeatable, Docker-free* IdP so the
// full browser chain (SSO login -> 401 step_up_required -> purpose=step_up
// redirect with prompt=login -> re-auth -> callback -> fragment grant) can
// run against any instance whose auth.oidc points here. Zero dependencies:
// node:http + node:crypto only (globals come in as explicit node: imports —
// the scripts/ lint carve-out only declares URL/console/process).
//
// Implements exactly the surface BinFlow consumes (ADR-0020 / T-219):
//   GET  /.well-known/openid-configuration   discovery (issuer/endpoints/jwks)
//   GET  /authorize?...                      HTML login form (echoes the query
//                                           through the form action; the page
//                                           carries data-testid="idp-login-
//                                           page" + data-prompt so specs can
//                                           assert prompt=login arrived)
//   POST /authenticate?<authorize query>     credential check -> 302 back to
//                                           redirect_uri with code+state
//   POST /token                              code exchange (single-use codes;
//                                           client auth accepts basic AND form
//                                           secret) -> access_token + RS256
//                                           id_token (preferred_username +
//                                           groups claims)
//   GET  /jwks.json                          the signing key (RSA-2048, boot-time)
//
// Usage: node scripts/mock-idp.mjs [port] [client-secret]
//   port           default 18095
//   client-secret  default 't260-mock-idp-secret' (BINFLOW_AUTH_OIDC_CLIENT_SECRET)
//
// Fixture identity (the only user): ssouser / ssopass-t260, groups ["bf-readonly"]
// -> with auth.oidc.readonly_group=bf-readonly the auto-created console user is
// a NON-admin session (the step-up scope) that can still read repos.

import { Buffer } from 'node:buffer'
import { createHash, createSign, generateKeyPairSync, randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
import { URL, URLSearchParams } from 'node:url'

const PORT = Number(process.argv[2] ?? 18095)
const CLIENT_SECRET = process.argv[3] ?? 't260-mock-idp-secret'
const ISSUER = `http://127.0.0.1:${PORT}`

const USER = { username: 'ssouser', password: 'ssopass-t260', groups: ['bf-readonly'] }
const CLIENT_ID = 'binflow-console'

// Boot-time signing key (RSA-2048). JWKS kid = sha256(SPKI DER) prefix.
const { publicKey, privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 })
const KID = createHash('sha256').update(publicKey.export({ type: 'spki', format: 'der' })).digest('hex').slice(0, 16)

const b64url = (buf) => Buffer.from(buf).toString('base64url')

/** RS256-sign one id_token (header.kid lets go-oidc pick the JWKS key). */
function signIdToken(payload) {
  const head = b64url(JSON.stringify({ alg: 'RS256', typ: 'JWT', kid: KID }))
  const body = b64url(JSON.stringify(payload))
  const input = `${head}.${body}`
  const sig = createSign('RSA-SHA256').update(input).sign(privateKey)
  return `${input}.${b64url(sig)}`
}

/** Single-use authorization codes: Map code -> redirect_uri (burned on exchange). */
const codes = new Map()

function sendJSON(res, status, obj) {
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
    Pragma: 'no-cache',
  })
  res.end(JSON.stringify(obj))
}

/** The login form: a real page the browser fills (the spec drives #idp-username
 *  / #idp-password / #idp-submit), echoing the authorize query through the form
 *  action so state/code_challenge/prompt survive the round trip. The query is
 *  appended RAW (already URL-encoded upstream) with only HTML attribute escaping
 *  — re-encoding it as one value would flatten the parameter structure. */
function loginPage(query) {
  const attr = (s) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;')
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>mock IdP — sign in</title></head>
<body data-testid="idp-login-page" data-prompt="${attr(new URLSearchParams(query).get('prompt') ?? '')}">
<h1>mock IdP</h1>
<form method="post" action="/authenticate?${attr(query)}">
  <label for="idp-username">Username</label>
  <input id="idp-username" name="username" autocomplete="username">
  <label for="idp-password">Password</label>
  <input id="idp-password" name="password" type="password" autocomplete="current-password">
  <button id="idp-submit" type="submit">Sign in</button>
</form>
</body></html>`
}

const server = createServer((req, res) => {
  const url = new URL(req.url, ISSUER)

  if (req.method === 'GET' && url.pathname === '/.well-known/openid-configuration') {
    return sendJSON(res, 200, {
      issuer: ISSUER,
      authorization_endpoint: `${ISSUER}/authorize`,
      token_endpoint: `${ISSUER}/token`,
      jwks_uri: `${ISSUER}/jwks.json`,
      response_types_supported: ['code'],
      subject_types_supported: ['public'],
      id_token_signing_alg_values_supported: ['RS256'],
    })
  }

  if (req.method === 'GET' && url.pathname === '/jwks.json') {
    const jwk = publicKey.export({ format: 'jwk' })
    return sendJSON(res, 200, { keys: [{ ...jwk, kid: KID, use: 'sig', alg: 'RS256' }] })
  }

  if (req.method === 'GET' && url.pathname === '/authorize') {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
    return res.end(loginPage(url.search.slice(1)))
  }

  if (req.method === 'POST' && url.pathname === '/authenticate') {
    let body = ''
    req.on('data', (c) => {
      body += c
    })
    return req.on('end', () => {
      const form = new URLSearchParams(body)
      if (form.get('username') !== USER.username || form.get('password') !== USER.password) {
        res.writeHead(401, { 'Content-Type': 'text/html; charset=utf-8' })
        return res.end('<html><body>invalid credentials (mock IdP)</body></html>')
      }
      const redirectUri = url.searchParams.get('redirect_uri')
      const state = url.searchParams.get('state')
      if (!redirectUri) {
        res.writeHead(400)
        return res.end('missing redirect_uri')
      }
      const code = randomBytes(24).toString('hex')
      codes.set(code, redirectUri)
      const back = new URL(redirectUri)
      back.searchParams.set('code', code)
      if (state) back.searchParams.set('state', state)
      res.writeHead(302, { Location: back.toString() })
      return res.end()
    })
  }

  if (req.method === 'POST' && url.pathname === '/token') {
    let body = ''
    req.on('data', (c) => {
      body += c
    })
    return req.on('end', () => {
      const form = new URLSearchParams(body)
      // Client auth: x/oauth2 auto-detects the style — accept both spellings.
      const auth = req.headers.authorization ?? ''
      const [scheme, b64] = auth.split(' ')
      let basicOk = false
      if (scheme === 'Basic' && b64) {
        const dec = Buffer.from(b64, 'base64').toString('utf8')
        const i = dec.indexOf(':')
        basicOk = dec.slice(0, i) === CLIENT_ID && dec.slice(i + 1) === CLIENT_SECRET
      }
      const formOk = form.get('client_id') === CLIENT_ID && form.get('client_secret') === CLIENT_SECRET
      const code = form.get('code')
      const redirectUri = codes.get(code)
      if ((!basicOk && !formOk) || form.get('grant_type') !== 'authorization_code' || !redirectUri) {
        return sendJSON(res, 401, { error: 'invalid_client', error_description: 'client auth or code rejected' })
      }
      codes.delete(code) // single-use
      const now = Math.floor(Date.now() / 1000)
      const idToken = signIdToken({
        iss: ISSUER,
        sub: USER.username,
        aud: CLIENT_ID,
        exp: now + 300,
        iat: now,
        preferred_username: USER.username,
        groups: USER.groups,
      })
      return sendJSON(res, 200, {
        access_token: `mock-access-${randomBytes(12).toString('hex')}`,
        token_type: 'Bearer',
        expires_in: 3600,
        id_token: idToken,
      })
    })
  }

  res.writeHead(404)
  res.end('not found (mock IdP)')
})

server.listen(PORT, '127.0.0.1', () => {
  process.stdout.write(`mock-idp: listening on ${ISSUER} (kid=${KID})\n`)
})
