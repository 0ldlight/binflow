// 流式 SHA-256（T-100 上传/下载对账用，零依赖）。
//
// 为什么不用 crypto.subtle.digest：SubtleCrypto 要求整块 ArrayBuffer，
// 1GB 文件会同时持有「响应/文件缓冲 + 哈希缓冲」，直接顶穿 T-104 的
// RSS<256MB 预算（FR-25-AC2）。本实现是 FIPS 180-4 的标准转录，按块
// 增量（update/digest 两段），内存上界 = 单块 4MB + 64B 状态。
//
// 性能：纯 JS 约 30~80MB/s（桌面 Chromium 实测量级），对控制台典型的
// 几十 MB 制品是亚秒级；1GB 腿（T-104）哈希与大文件上传并行进行，不
// 阻塞上传流的启动（先起 PUT，边读边补 header 的形态浏览器做不到——
// X-Checksum-Sha256 是请求头，必须先算完；接受该串行成本，checkbox
// 可关）。

/** SHA-256 轮常量（FIPS 180-4 section 4.2.2） */
const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

/** 增量 SHA-256 上下文：update 任意次（Uint8Array 视图即可），digest 一次 */
export class Sha256 {
  private readonly h = new Uint32Array(8)
  private readonly w = new Uint32Array(64)
  private readonly block = new Uint8Array(64)
  private blockLen = 0
  private bytes = 0

  constructor() {
    this.h.set([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19])
  }

  update(data: Uint8Array): this {
    this.bytes += data.length
    let offset = 0
    if (this.blockLen > 0) {
      const take = Math.min(64 - this.blockLen, data.length)
      this.block.set(data.subarray(0, take), this.blockLen)
      this.blockLen += take
      offset = take
      if (this.blockLen < 64) return this
      this.compress(this.block, 0)
      this.blockLen = 0
    }
    while (offset + 64 <= data.length) {
      this.compress(data, offset)
      offset += 64
    }
    if (offset < data.length) {
      this.block.set(data.subarray(offset))
      this.blockLen = data.length - offset
    }
    return this
  }

  digest(): string {
    // 长度是字节计数（< 2^53 精确），比特长拆 32+32 写入填充块
    const bitLen = this.bytes * 8
    const hi = Math.floor(bitLen / 4294967296)
    const lo = bitLen % 4294967296
    this.block[this.blockLen++] = 0x80
    if (this.blockLen > 56) {
      this.block.fill(0, this.blockLen)
      this.compress(this.block, 0)
      this.blockLen = 0
    }
    this.block.fill(0, this.blockLen, 56)
    this.block[56] = (hi >>> 24) & 0xff
    this.block[57] = (hi >>> 16) & 0xff
    this.block[58] = (hi >>> 8) & 0xff
    this.block[59] = hi & 0xff
    this.block[60] = (lo / 16777216) & 0xff
    this.block[61] = (lo / 65536) & 0xff
    this.block[62] = (lo / 256) & 0xff
    this.block[63] = lo & 0xff
    this.compress(this.block, 0)
    let out = ''
    for (let i = 0; i < 8; i++) {
      out += this.h[i].toString(16).padStart(8, '0')
    }
    return out
  }

  /** 压缩函数；data[offset, offset+64) 必须恰好是一个完整块 */
  private compress(data: Uint8Array, offset: number): void {
    const w = this.w
    for (let i = 0; i < 16; i++) {
      const j = offset + i * 4
      w[i] = ((data[j] << 24) | (data[j + 1] << 16) | (data[j + 2] << 8) | data[j + 3]) >>> 0
    }
    for (let i = 16; i < 64; i++) {
      const x = w[i - 15]
      const y = w[i - 2]
      const s0 = ((x >>> 7) | (x << 25)) ^ ((x >>> 18) | (x << 14)) ^ (x >>> 3)
      const s1 = ((y >>> 17) | (y << 15)) ^ ((y >>> 19) | (y << 13)) ^ (y >>> 10)
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0
    }
    let a = this.h[0]
    let b = this.h[1]
    let c = this.h[2]
    let d = this.h[3]
    let e = this.h[4]
    let f = this.h[5]
    let g = this.h[6]
    let h = this.h[7]
    for (let i = 0; i < 64; i++) {
      const S1 = ((e >>> 6) | (e << 26)) ^ ((e >>> 11) | (e << 21)) ^ ((e >>> 25) | (e << 7))
      const ch = (e & f) ^ (~e & g)
      const t1 = (h + S1 + ch + K[i] + w[i]) | 0
      const S0 = ((a >>> 2) | (a << 30)) ^ ((a >>> 13) | (a << 19)) ^ ((a >>> 22) | (a << 10))
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (S0 + maj) | 0
      h = g
      g = f
      f = e
      e = (d + t1) | 0
      d = c
      c = b
      b = a
      a = (t1 + t2) | 0
    }
    this.h[0] = (this.h[0] + a) | 0
    this.h[1] = (this.h[1] + b) | 0
    this.h[2] = (this.h[2] + c) | 0
    this.h[3] = (this.h[3] + d) | 0
    this.h[4] = (this.h[4] + e) | 0
    this.h[5] = (this.h[5] + f) | 0
    this.h[6] = (this.h[6] + g) | 0
    this.h[7] = (this.h[7] + h) | 0
  }
}

/** 文件/Blob 的流式 sha256（4MB 块切片读，内存有界） */
export async function blobSha256(file: Blob, chunkSize = 4 << 20): Promise<string> {
  const hasher = new Sha256()
  if (file.size === 0) return hasher.digest()
  for (let off = 0; off < file.size; off += chunkSize) {
    const buf = await file.slice(off, off + chunkSize).arrayBuffer()
    hasher.update(new Uint8Array(buf))
  }
  return hasher.digest()
}

/** ReadableStream 的流式 sha256（下载对账：边收边哈希，不另驻留整块） */
export async function streamSha256(body: ReadableStream<Uint8Array>): Promise<string> {
  const hasher = new Sha256()
  const reader = body.getReader()
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    if (value && value.length > 0) hasher.update(value)
  }
  return hasher.digest()
}
