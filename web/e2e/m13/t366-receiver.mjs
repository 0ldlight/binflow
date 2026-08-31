#!/usr/bin/env node
// T-366 consumer-leg receiver (M13 FR-115.7 / L08): the script HTTP receiver
// the ephemeral pro instance delivers webhooks to. Node stdlib only — it runs
// as a child process of t366-consumer.spec.ts.
//
// Endpoints (modeled by PATH so one process serves every leg):
//   POST /ok       → record + 200 {"ok":true}
//   POST /flaky    → record; first 2 requests answer 500, then 200 (the
//                    fixed-interval retry-then-recover leg)
//   POST /fail500  → record + always 500 (the dead-letter leg: 5 attempts)
//   POST /hang     → record; hold the socket ~90s before answering (the
//                    timeout-fault leg — the 30s attempt budget aborts first)
//   GET  /__log?path=/ok → the recorded requests (headers + raw body bytes
//                    preserved as latin1 so the HMAC leg verifies the EXACT
//                    wire bytes; JSON-parsed view alongside)
//   GET  /__health → liveness for the spec's boot wait
//
// Usage: node t366-receiver.mjs [port] [host]   (port 0 = ephemeral; host
// default 127.0.0.1 — the consumer spec passes the machine's GLOBAL IPv6 so
// the SSRF guard admits the target with its default posture; the port is
// printed on stdout as "RECV <port>" the moment the listener is up)

import { createServer } from 'node:http'

const port = Number(process.argv[2] ?? 0)
const host = process.argv[3] ?? '127.0.0.1'

/** path → [{ ts, headers, raw, body }] */
const log = new Map()
const counts = new Map()

const bump = (p) => {
  const n = (counts.get(p) ?? 0) + 1
  counts.set(p, n)
  return n
}

const server = createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://receiver')
  const path = url.pathname

  if (req.method === 'GET' && path === '/__health') {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ ok: true, seen: Object.fromEntries(counts) }))
    return
  }

  if (req.method === 'GET' && path === '/__log') {
    const want = url.searchParams.get('path') ?? ''
    const entries = log.get(want) ?? []
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(entries))
    return
  }

  if (req.method === 'GET' && path === '/__reset') {
    log.clear()
    counts.clear()
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end('{}')
    return
  }

  if (req.method !== 'POST') {
    res.writeHead(405)
    res.end()
    return
  }

  const chunks = []
  req.on('data', (c) => chunks.push(c))
  req.on('end', () => {
    const buf = Buffer.concat(chunks)
    const entries = log.get(path) ?? []
    let parsed = null
    try {
      parsed = JSON.parse(buf.toString('utf8'))
    } catch {
      parsed = null
    }
    entries.push({
      ts: Date.now(),
      method: req.method,
      headers: req.headers,
      // latin1 round-trips arbitrary bytes through JSON without loss — the
      // HMAC leg re-buffers this to the exact wire bytes it verifies against.
      raw: buf.toString('latin1'),
      body: parsed,
    })
    log.set(path, entries)
    const n = bump(path)

    if (path === '/fail500') {
      res.writeHead(500, { 'Content-Type': 'text/plain' })
      res.end('injected fault: always 500')
      return
    }
    if (path === '/flaky' && n <= 2) {
      res.writeHead(500, { 'Content-Type': 'text/plain' })
      res.end(`injected fault: 500 on attempt ${n}`)
      return
    }
    if (path === '/hang') {
      // Hold past the engine's 30s whole-attempt budget; the client aborts and
      // the attempt lands as a send failure (timeout fault).
      setTimeout(() => {
        try {
          res.writeHead(200, { 'Content-Type': 'application/json' })
          res.end('{"ok":true,"late":true}')
        } catch {
          // socket already gone (the aborted attempt)
        }
      }, 90_000)
      return
    }
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ ok: true, n }))
  })
})

server.listen(port, host, () => {
  const addr = server.address()
  process.stdout.write(`RECV ${addr.port}\n`)
})
