//! mlx-quant-kompress: zero-dependency SafeTensors -> 5-stage ternary-ish
//! packed blob exporter, with Ollama blob/create push.
//!
//! Stages: sphere_pop -> cross_laminate -> die_mold_1 -> insert_4 CRC-8 ->
//! die_mold_2. Decoding of BF16/F16 is integer bit manipulation only.

use std::env;
use std::fs;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::path::{Path, PathBuf};

const MAGIC: &[u8; 4] = b"KMP1";

fn main() {
    if let Err(e) = run() {
        eprintln!("mlx-quant-kompress: {e}");
        std::process::exit(1);
    }
}

fn run() -> Result<(), String> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        return usage();
    }
    match args[1].as_str() {
        "export" => {
            let input = flag(&args, "--input").ok_or("export needs --input")?;
            let output = flag(&args, "--output").unwrap_or_else(|| ".".into());
            let name = flag(&args, "--name").unwrap_or_else(|| "kompress".into());
            export(&input, &output, &name)
        }
        "push" => {
            let blob = flag(&args, "--blob").ok_or("push needs --blob")?;
            let name = flag(&args, "--name").ok_or("push needs --name")?;
            let base = flag(&args, "--ollama").unwrap_or_else(|| "http://127.0.0.1:11434".into());
            push(&blob, &name, &base)
        }
        "digest" => {
            let blob = flag(&args, "--blob").ok_or("digest needs --blob")?;
            let b = fs::read(&blob).map_err(|e| e.to_string())?;
            println!("{}", hex(&sha256(&b)));
            Ok(())
        }
        _ => usage(),
    }
}

fn usage() -> Result<(), String> {
    eprintln!("usage: mlx-quant-kompress export --input model.safetensors --output dir [--name kompress]");
    eprintln!("       mlx-quant-kompress push --blob model.kmp --name model [--ollama http://127.0.0.1:11434]");
    eprintln!("       mlx-quant-kompress digest --blob model.kmp");
    Err("bad arguments".into())
}

fn flag(args: &[String], name: &str) -> Option<String> {
    args.iter().position(|a| a == name).and_then(|i| args.get(i + 1)).cloned()
}

fn export(input: &str, output: &str, name: &str) -> Result<(), String> {
    let bytes = fs::read(input).map_err(|e| format!("read {input}: {e}"))?;
    let (header, payload) = parse_safetensors(&bytes)?;
    let mut packed_all = Vec::new();
    for (tname, t) in &header {
        let p = quantize_tensor(t, payload)?;
        packed_all.extend_from_slice(&p);
        eprintln!("tensor {tname}: {} -> {} bytes", t.count(), p.len());
    }
    let half = (packed_all.len() + 1) / 2;
    let (a, b) = packed_all.split_at(half);
    let laminated = cross_laminate(a, b);
    let sphere = sphere_pop(&laminated);
    let crc = crc8(&laminated);
    let blob = die_mold_2(&laminated, sphere, crc);
    let source_sha = sha256(&bytes);

    let outdir = PathBuf::from(output);
    fs::create_dir_all(&outdir).map_err(|e| e.to_string())?;
    let kmp = outdir.join(format!("{name}.kmp"));
    fs::write(&kmp, &blob).map_err(|e| e.to_string())?;
    let st = outdir.join(format!("{name}.safetensors"));
    write_safetensors(&st, &blob, &source_sha, sphere, crc)?;
    println!("blob={} safetensors={} bytes={} sha256={}", kmp.display(), st.display(), blob.len(), hex(&sha256(&blob)));
    Ok(())
}

#[derive(Debug, Clone)]
struct Tensor {
    dtype: String,
    shape: Vec<usize>,
    start: usize,
    end: usize,
}

impl Tensor {
    fn count(&self) -> usize {
        if self.shape.is_empty() { 1 } else { self.shape.iter().product() }
    }
}

fn parse_safetensors(b: &[u8]) -> Result<(Vec<(String, Tensor)>, &[u8]), String> {
    if b.len() < 8 { return Err("short safetensors".into()); }
    let hlen = u64::from_le_bytes(b[..8].try_into().unwrap()) as usize;
    if hlen == 0 || hlen > 100 << 20 || 8 + hlen > b.len() { return Err("bad header length".into()); }
    let header = std::str::from_utf8(&b[8..8 + hlen]).map_err(|e| e.to_string())?;
    let json = parse_json(header)?;
    let obj = match json { Json::Obj(o) => o, _ => return Err("header is not an object".into()) };
    let payload = &b[8 + hlen..];
    let mut out = Vec::new();
    for (name, val) in obj {
        if name == "__metadata__" { continue; }
        let t = tensor_from_json(&val)?;
        if t.end > payload.len() { return Err(format!("tensor {name} out of range")); }
        out.push((name, t));
    }
    Ok((out, payload))
}

fn tensor_from_json(v: &Json) -> Result<Tensor, String> {
    let dtype = v.get("dtype").and_then(Json::as_str).ok_or("missing dtype")?.to_string();
    let shape = v.get("shape").and_then(Json::as_arr).ok_or("missing shape")?
        .iter().map(|x| x.as_f64().unwrap_or(0.0) as usize).collect();
    let offs = v.get("data_offsets").and_then(Json::as_arr).ok_or("missing data_offsets")?;
    if offs.len() != 2 { return Err("bad data_offsets".into()); }
    Ok(Tensor { dtype, shape, start: offs[0].as_f64().unwrap_or(0.0) as usize, end: offs[1].as_f64().unwrap_or(0.0) as usize })
}

fn quantize_tensor(t: &Tensor, payload: &[u8]) -> Result<Vec<u8>, String> {
    let n = t.count();
    let mut max_abs = 0.0f32;
    for i in 0..n {
        let v = decode_at(&t.dtype, payload, t.start, i)?;
        if v.abs() > max_abs { max_abs = v.abs(); }
    }
    let scale = if max_abs == 0.0 { 1.0 } else { max_abs / 7.0 };
    let mut out = Vec::with_capacity((n + 1) / 2);
    let mut cur = 0u8;
    for i in 0..n {
        let v = decode_at(&t.dtype, payload, t.start, i)?;
        let q = (v / scale).round().clamp(-8.0, 7.0) as i8;
        let nib = (q as u8) & 0x0f;
        if i % 2 == 0 {
            cur = nib;
        } else {
            out.push(cur | (nib << 4));
            cur = 0;
        }
    }
    if n % 2 == 1 { out.push(cur); }
    Ok(out)
}

fn decode_at(dtype: &str, payload: &[u8], base: usize, i: usize) -> Result<f32, String> {
    match dtype {
        "F32" => {
            let o = base + i * 4;
            if o + 4 > payload.len() { return Err("F32 out of range".into()); }
            Ok(f32::from_le_bytes(payload[o..o + 4].try_into().unwrap()))
        }
        "F16" => {
            let o = base + i * 2;
            if o + 2 > payload.len() { return Err("F16 out of range".into()); }
            Ok(half_to_f32(u16::from_le_bytes(payload[o..o + 2].try_into().unwrap())))
        }
        "BF16" => {
            let o = base + i * 2;
            if o + 2 > payload.len() { return Err("BF16 out of range".into()); }
            Ok(f32::from_bits((u16::from_le_bytes(payload[o..o + 2].try_into().unwrap()) as u32) << 16))
        }
        "I8" => {
            let o = base + i;
            if o >= payload.len() { return Err("I8 out of range".into()); }
            Ok(payload[o] as i8 as f32)
        }
        "U8" => {
            let o = base + i;
            if o >= payload.len() { return Err("U8 out of range".into()); }
            Ok(payload[o] as f32)
        }
        "I64" => {
            let o = base + i * 8;
            if o + 8 > payload.len() { return Err("I64 out of range".into()); }
            Ok(i64::from_le_bytes(payload[o..o + 8].try_into().unwrap()) as f32)
        }
        "F64" => {
            let o = base + i * 8;
            if o + 8 > payload.len() { return Err("F64 out of range".into()); }
            Ok(f64::from_le_bytes(payload[o..o + 8].try_into().unwrap()) as f32)
        }
        other => Err(format!("unsupported dtype {other}")),
    }
}

fn half_to_f32(h: u16) -> f32 {
    let sign = ((h >> 15) & 1) as u32;
    let exp = ((h >> 10) & 0x1f) as u32;
    let frac = (h & 0x3ff) as u32;
    let bits = if exp == 0 {
        if frac == 0 {
            sign << 31
        } else {
            let mut e = -1i32;
            let mut f = frac;
            while f & 0x400 == 0 { f <<= 1; e += 1; }
            (sign << 31) | (((127 - 15 + e) as u32) << 23) | ((f & 0x3ff) << 13)
        }
    } else if exp == 31 {
        (sign << 31) | (0xff << 23) | (frac << 13)
    } else {
        (sign << 31) | ((exp + 127 - 15) << 23) | (frac << 13)
    };
    f32::from_bits(bits)
}

fn cross_laminate(a: &[u8], b: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(a.len() + b.len());
    for i in 0..a.len().max(b.len()) {
        if i < a.len() { out.push(a[i]); }
        if i < b.len() { out.push(b[i]); }
    }
    out
}

fn sphere_pop(data: &[u8]) -> u8 {
    let mut h = 0u8;
    for &b in data {
        h ^= popcount_u64(b as u64) as u8;
    }
    h
}

#[cfg(target_arch = "x86_64")]
fn popcount_u64(x: u64) -> u32 {
    let out: u64;
    unsafe { core::arch::asm!("popcnt {0}, {1}", out(reg) out, in(reg) x, options(nostack, nomem)); }
    out as u32
}

#[cfg(target_arch = "aarch64")]
fn popcount_u64(x: u64) -> u32 {
    let out: u32;
    unsafe {
        core::arch::asm!(
            "fmov d0, {x}",
            "cnt v0.8b, v0.8b",
            "addv b0, v0.8b",
            "fmov {out:w}, s0",
            x = in(reg) x,
            out = out(reg) out,
            out("d0") _,
        );
    }
    out
}

#[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
fn popcount_u64(x: u64) -> u32 { x.count_ones() }

fn crc8(data: &[u8]) -> u8 {
    let mut crc = 0u8;
    for &b in data {
        crc ^= b;
        for _ in 0..8 {
            crc = if crc & 0x80 != 0 { (crc << 1) ^ 0x07 } else { crc << 1 };
        }
    }
    crc
}

fn die_mold_2(data: &[u8], sphere: u8, crc: u8) -> Vec<u8> {
    let mut out = Vec::with_capacity(12 + data.len());
    out.extend_from_slice(MAGIC);
    out.push(1);
    out.push(sphere);
    out.push(crc);
    out.push(0);
    out.extend_from_slice(&(data.len() as u32).to_le_bytes());
    out.extend_from_slice(data);
    out
}

fn write_safetensors(path: &Path, blob: &[u8], source_sha: &[u8; 32], sphere: u8, crc: u8) -> Result<(), String> {
    let header = format!(
        "{{\"blob\":{{\"dtype\":\"U8\",\"shape\":[{}],\"data_offsets\":[0,{}]}},\"__metadata__\":{{\"format\":\"mince-kompress-v1\",\"source_sha256\":\"{}\",\"sphere_pop\":\"{:02x}\",\"crc8\":\"{:02x}\"}}}}",
        blob.len(), blob.len(), hex(source_sha), sphere, crc
    );
    let mut out = Vec::with_capacity(8 + header.len() + blob.len());
    out.extend_from_slice(&(header.len() as u64).to_le_bytes());
    out.extend_from_slice(header.as_bytes());
    out.extend_from_slice(blob);
    fs::write(path, out).map_err(|e| e.to_string())
}

fn push(blob_path: &str, name: &str, base: &str) -> Result<(), String> {
    let blob = fs::read(blob_path).map_err(|e| e.to_string())?;
    let digest = hex(&sha256(&blob));
    let url = format!("{}/api/blobs/sha256:{}", base.trim_end_matches('/'), digest);
    let resp = http_post(&url, &blob, "application/octet-stream")?;
    eprintln!("blob {} -> {} bytes response", digest, resp.len());
    let create = format!(
        "{{\"name\":\"{}\",\"files\":{{\"blob\":\"sha256:{}\"}}}}",
        json_escape(name), digest
    );
    let resp = http_post(&format!("{}/api/create", base.trim_end_matches('/')), create.as_bytes(), "application/json")?;
    print!("{}", String::from_utf8_lossy(&resp));
    Ok(())
}

fn http_post(url: &str, body: &[u8], content_type: &str) -> Result<Vec<u8>, String> {
    let rest = url.strip_prefix("http://").ok_or("only http:// is supported")?;
    let (hostport, path) = match rest.find('/') {
        Some(i) => (&rest[..i], &rest[i..]),
        None => (rest, "/"),
    };
    let (host, port) = match hostport.rsplit_once(':') {
        Some((h, p)) => (h, p.parse::<u16>().map_err(|e| e.to_string())?),
        None => (hostport, 80),
    };
    let mut stream = TcpStream::connect((host, port)).map_err(|e| e.to_string())?;
    let req = format!(
        "POST {path} HTTP/1.1\r\nHost: {hostport}\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    );
    stream.write_all(req.as_bytes()).map_err(|e| e.to_string())?;
    stream.write_all(body).map_err(|e| e.to_string())?;
    let mut resp = Vec::new();
    stream.read_to_end(&mut resp).map_err(|e| e.to_string())?;
    let split = resp.windows(4).position(|w| w == b"\r\n\r\n").ok_or("bad http response")?;
    let head = String::from_utf8_lossy(&resp[..split]);
    if !head.starts_with("HTTP/1.1 200") && !head.starts_with("HTTP/1.0 200") {
        return Err(format!("http error: {}", head.lines().next().unwrap_or("?")));
    }
    Ok(resp[split + 4..].to_vec())
}

fn json_escape(s: &str) -> String {
    s.replace('\\', "\\\\").replace('"', "\\\"")
}

// ---- minimal JSON parser ----

#[derive(Debug, Clone)]
enum Json { Null, Bool, Num(f64), Str(String), Arr(Vec<Json>), Obj(Vec<(String, Json)>) }

impl Json {
    fn get(&self, key: &str) -> Option<&Json> {
        match self { Json::Obj(o) => o.iter().find(|(k, _)| k == key).map(|(_, v)| v), _ => None }
    }
    fn as_str(&self) -> Option<&str> { if let Json::Str(s) = self { Some(s) } else { None } }
    fn as_arr(&self) -> Option<&[Json]> { if let Json::Arr(a) = self { Some(a) } else { None } }
    fn as_f64(&self) -> Option<f64> { if let Json::Num(n) = self { Some(*n) } else { None } }
}

fn parse_json(s: &str) -> Result<Json, String> {
    let mut p = Parser { b: s.as_bytes(), i: 0 };
    let v = p.value()?;
    p.ws();
    if p.i != p.b.len() { return Err("trailing json".into()); }
    Ok(v)
}

struct Parser<'a> { b: &'a [u8], i: usize }

impl<'a> Parser<'a> {
    fn ws(&mut self) { while self.i < self.b.len() && self.b[self.i].is_ascii_whitespace() { self.i += 1; } }
    fn value(&mut self) -> Result<Json, String> {
        self.ws();
        match self.b.get(self.i).copied() {
            Some(b'{') => self.object(),
            Some(b'[') => self.array(),
            Some(b'"') => Ok(Json::Str(self.string()?)),
            Some(b't') => { self.lit("true")?; Ok(Json::Bool) }
            Some(b'f') => { self.lit("false")?; Ok(Json::Bool) }
            Some(b'n') => { self.lit("null")?; Ok(Json::Null) }
            Some(_) => self.number(),
            None => Err("unexpected eof".into()),
        }
    }
    fn lit(&mut self, s: &str) -> Result<(), String> {
        if self.b[self.i..].starts_with(s.as_bytes()) { self.i += s.len(); Ok(()) } else { Err("bad literal".into()) }
    }
    fn string(&mut self) -> Result<String, String> {
        self.i += 1;
        let mut out = String::new();
        while self.i < self.b.len() {
            let c = self.b[self.i]; self.i += 1;
            match c {
                b'"' => return Ok(out),
                b'\\' => {
                    let e = *self.b.get(self.i).ok_or("bad escape")?; self.i += 1;
                    match e {
                        b'"' => out.push('"'), b'\\' => out.push('\\'), b'/' => out.push('/'),
                        b'b' => out.push('\u{8}'), b'f' => out.push('\u{c}'), b'n' => out.push('\n'),
                        b'r' => out.push('\r'), b't' => out.push('\t'),
                        b'u' => {
                            let h = std::str::from_utf8(self.b.get(self.i..self.i + 4).ok_or("bad unicode")?).map_err(|e| e.to_string())?;
                            self.i += 4;
                            let cp = u32::from_str_radix(h, 16).map_err(|e| e.to_string())?;
                            out.push(char::from_u32(cp).unwrap_or('\u{fffd}'));
                        }
                        _ => return Err("bad escape".into()),
                    }
                }
                _ => out.push(c as char),
            }
        }
        Err("unterminated string".into())
    }
    fn number(&mut self) -> Result<Json, String> {
        let start = self.i;
        while self.i < self.b.len() && matches!(self.b[self.i], b'-' | b'+' | b'.' | b'e' | b'E' | b'0'..=b'9') { self.i += 1; }
        let s = std::str::from_utf8(&self.b[start..self.i]).map_err(|e| e.to_string())?;
        s.parse::<f64>().map(Json::Num).map_err(|e| e.to_string())
    }
    fn array(&mut self) -> Result<Json, String> {
        self.i += 1;
        let mut out = Vec::new();
        self.ws();
        if self.b.get(self.i) == Some(&b']') { self.i += 1; return Ok(Json::Arr(out)); }
        loop {
            out.push(self.value()?);
            self.ws();
            match self.b.get(self.i) {
                Some(b',') => { self.i += 1; }
                Some(b']') => { self.i += 1; return Ok(Json::Arr(out)); }
                _ => return Err("bad array".into()),
            }
        }
    }
    fn object(&mut self) -> Result<Json, String> {
        self.i += 1;
        let mut out = Vec::new();
        self.ws();
        if self.b.get(self.i) == Some(&b'}') { self.i += 1; return Ok(Json::Obj(out)); }
        loop {
            self.ws();
            let key = self.string()?;
            self.ws();
            if self.b.get(self.i) != Some(&b':') { return Err("missing colon".into()); }
            self.i += 1;
            let val = self.value()?;
            out.push((key, val));
            self.ws();
            match self.b.get(self.i) {
                Some(b',') => { self.i += 1; }
                Some(b'}') => { self.i += 1; return Ok(Json::Obj(out)); }
                _ => return Err("bad object".into()),
            }
        }
    }
}

// ---- SHA-256 (zero dependency) ----

const K: [u32; 64] = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

fn sha256(data: &[u8]) -> [u8; 32] {
    let mut h: [u32; 8] = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
    let mut msg = data.to_vec();
    let bitlen = (data.len() as u64) * 8;
    msg.push(0x80);
    while msg.len() % 64 != 56 { msg.push(0); }
    msg.extend_from_slice(&bitlen.to_be_bytes());
    for chunk in msg.chunks(64) {
        let mut w = [0u32; 64];
        for i in 0..16 { w[i] = u32::from_be_bytes(chunk[i * 4..i * 4 + 4].try_into().unwrap()); }
        for i in 16..64 {
            let s0 = w[i - 15].rotate_right(7) ^ w[i - 15].rotate_right(18) ^ (w[i - 15] >> 3);
            let s1 = w[i - 2].rotate_right(17) ^ w[i - 2].rotate_right(19) ^ (w[i - 2] >> 10);
            w[i] = w[i - 16].wrapping_add(s0).wrapping_add(w[i - 7]).wrapping_add(s1);
        }
        let (mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut hh) = (h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7]);
        for i in 0..64 {
            let s1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
            let ch = (e & f) ^ ((!e) & g);
            let t1 = hh.wrapping_add(s1).wrapping_add(ch).wrapping_add(K[i]).wrapping_add(w[i]);
            let s0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
            let maj = (a & b) ^ (a & c) ^ (b & c);
            let t2 = s0.wrapping_add(maj);
            hh = g; g = f; f = e; e = d.wrapping_add(t1);
            d = c; c = b; b = a; a = t1.wrapping_add(t2);
        }
        h[0] = h[0].wrapping_add(a); h[1] = h[1].wrapping_add(b); h[2] = h[2].wrapping_add(c); h[3] = h[3].wrapping_add(d);
        h[4] = h[4].wrapping_add(e); h[5] = h[5].wrapping_add(f); h[6] = h[6].wrapping_add(g); h[7] = h[7].wrapping_add(hh);
    }
    let mut out = [0u8; 32];
    for (i, v) in h.iter().enumerate() { out[i * 4..i * 4 + 4].copy_from_slice(&v.to_be_bytes()); }
    out
}

fn hex(b: &[u8]) -> String {
    let mut s = String::with_capacity(b.len() * 2);
    for x in b { s.push_str(&format!("{x:02x}")); }
    s
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sha256_known() {
        assert_eq!(hex(&sha256(b"abc")), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    }

    #[test]
    fn half_decode() {
        assert_eq!(half_to_f32(0x3c00), 1.0);
        assert_eq!(half_to_f32(0xc000), -2.0);
    }

    #[test]
    fn crc8_known() {
        assert_eq!(crc8(b"123456789"), 0xf4);
    }

    #[test]
    fn json_parse_header() {
        let j = parse_json(r#"{"a":{"dtype":"U8","shape":[2],"data_offsets":[0,2]}}"#).unwrap();
        let t = tensor_from_json(j.get("a").unwrap()).unwrap();
        assert_eq!(t.dtype, "U8");
        assert_eq!(t.count(), 2);
    }
}
