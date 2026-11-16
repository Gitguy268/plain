// ---- Plain runtime ----
const $io = globalThis.__plainIO ?? {
  out: (s) => console.log(s),
  err: (s) => console.error(s),
  exit: (code) => { if (globalThis.process) globalThis.process.exitCode = code; },
};
class PlainPanic extends Error {
  constructor(message, loc) { super(message); this.loc = loc; }
}
class $Early { constructor(v) { this.v = v; } }
const Some = (_0) => ({ $: "Some", _0 });
const None = Object.freeze({ $: "None" });
const Ok = (_0) => ({ $: "Ok", _0 });
const Err = (_0) => ({ $: "Err", _0 });

function $panic(message, loc) { throw new PlainPanic(message, loc); }
function $try(r) {
  if (r.$ === "Ok" || r.$ === "Some") return r._0;
  throw new $Early(r);
}
const $MAX = 9007199254740991;
function $int(n, loc) {
  if (!Number.isSafeInteger(n)) {
    $panic(`Int overflow: the result is outside the range an Int can hold (-${$MAX} to ${$MAX}). Use Floats if you need bigger numbers.`, loc);
  }
  return n;
}
function $mod(a, b, loc) {
  if (b === 0) $panic("Division by zero in %.", loc);
  const m = a % b;
  return m !== 0 && (m < 0) !== (b < 0) ? m + b : m;
}
function $div(a, b, loc) {
  if (b === 0) $panic("Division by zero in div.", loc);
  return Math.floor(a / b);
}
function $to_float(n) { return n; }
function $parse_int(s) {
  const t = s.trim();
  if (!/^[+-]?\d+$/.test(t)) return Err(`"${s}" is not a whole number`);
  const n = Number(t);
  if (!Number.isSafeInteger(n)) return Err(`"${s}" is too big for an Int`);
  return Ok(n);
}
function $parse_float(s) {
  const t = s.trim();
  if (!/^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(t)) return Err(`"${s}" is not a number`);
  return Ok(Number(t));
}

function $isUnion(v) { return v !== null && typeof v === "object" && !Array.isArray(v) && !(v instanceof Map) && "$" in v; }
function $eq(a, b) {
  if (a === b) return true;
  if (typeof a !== "object" || typeof b !== "object" || a === null || b === null) return a !== a && b !== b;
  if (Array.isArray(a)) {
    if (!Array.isArray(b) || a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (!$eq(a[i], b[i])) return false;
    return true;
  }
  if (a instanceof Map) {
    if (!(b instanceof Map) || a.size !== b.size) return false;
    for (const [k, [, v]] of a) { const o = b.get(k); if (!o || !$eq(v, o[1])) return false; }
    return true;
  }
  const ka = Object.keys(a), kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  for (const k of ka) if (!$eq(a[k], b[k])) return false;
  return true;
}
function $cmp(a, b) {
  if (typeof a === "number" || typeof a === "string" || typeof a === "boolean") return a < b ? -1 : a > b ? 1 : 0;
  if (Array.isArray(a)) {
    for (let i = 0; i < Math.min(a.length, b.length); i++) { const c = $cmp(a[i], b[i]); if (c) return c; }
    return a.length - b.length;
  }
  if (a && typeof a === "object") {
    const ks = Object.keys(a).sort();
    for (const k of ks) { const c = $cmp(a[k], b[k]); if (c) return c; }
  }
  return 0;
}
// d describes where Floats are ("f", ["L", d], ["D", k, v], ["U", {Ctor: [d...]}], ["R", {field: d}]),
// so a Float that holds a whole number prints as 2.0, not 2.
function $repr(v, d) {
  if (typeof v === "string") return JSON.stringify(v);
  return $show(v, d);
}
function $show(v, d) {
  switch (typeof v) {
    case "string": return v;
    case "number":
      if (Object.is(v, -0)) return d === "f" ? "-0.0" : "0";
      return d === "f" && Number.isInteger(v) ? v + ".0" : String(v);
    case "boolean": return String(v);
    case "undefined": return "{}";
    case "function": return "<function>";
  }
  const sub = Array.isArray(d) ? d : [];
  if (Array.isArray(v)) return "[" + v.map((x) => $repr(x, sub[1])).join(", ") + "]";
  if (v instanceof Map) return "dict.from_list([" + [...v.values()].map(([k, x]) => `{key: ${$repr(k, sub[1])}, value: ${$repr(x, sub[2])}}`).join(", ") + "])";
  if ($isUnion(v)) {
    const fields = (sub[1] && sub[1][v.$]) || [];
    const args = [];
    for (let i = 0; ("_" + i) in v; i++) args.push($repr(v["_" + i], fields[i]));
    return args.length ? `${v.$}(${args.join(", ")})` : v.$;
  }
  const fields = sub[1] || {};
  const ks = Object.keys(v).sort();
  return "{" + ks.map((k) => `${k}: ${$repr(v[k], fields[k])}`).join(", ") + "}";
}
function $print(v, d) { $io.out($show(v, d)); }

function $assert(ok, src, loc) {
  if (!ok) throw new PlainPanic(`assert failed: ${src}`, loc);
}
function $assertEq(a, b, same, src, loc) {
  if ($eq(a, b) !== same) {
    throw new PlainPanic(`assert failed: ${src}\n  left:  ${$repr(a)}\n  right: ${$repr(b)}`, loc);
  }
}

// Values coming back from JavaScript externs are checked so null can't sneak in.
function $ext_opt(v) { return v === null || v === undefined ? None : Some(v); }
function $ext_res(f) {
  try { return Ok(f()); } catch (e) { return Err(String(e && e.message !== undefined ? e.message : e)); }
}
function $ext_val(v, name) {
  if (v === null || v === undefined) {
    $panic(`The JavaScript function behind extern ${name} returned ${v}, but its type says it always gives a value. Declare its return type as an Option.`);
  }
  return v;
}
function $ext_int(v, name) {
  if (!Number.isSafeInteger(v)) $panic(`The JavaScript function behind extern ${name} returned ${v}, which is not an Int. Declare its return type as Float.`);
  return v;
}

const $chars = (s) => Array.from(s);
const $list = {
  len: (xs) => xs.length,
  is_empty: (xs) => xs.length === 0,
  get: (xs, i) => (Number.isInteger(i) && i >= 0 && i < xs.length ? Some(xs[i]) : None),
  first: (xs) => (xs.length ? Some(xs[0]) : None),
  last: (xs) => (xs.length ? Some(xs[xs.length - 1]) : None),
  map: (xs, f) => xs.map((x) => f(x)),
  filter: (xs, f) => xs.filter((x) => f(x)),
  fold: (xs, start, f) => xs.reduce((acc, x) => f(acc, x), start),
  each: (xs, f) => { for (const x of xs) f(x); },
  push: (xs, x) => [...xs, x],
  reverse: (xs) => [...xs].reverse(),
  sort: (xs) => [...xs].sort($cmp),
  sort_by: (xs, key) => [...xs].sort((a, b) => $cmp(key(a), key(b))),
  range: (from, to) => { const out = []; for (let i = from; i <= to; i++) out.push(i); return out; },
  indices: (xs) => xs.map((_, i) => i),
  contains: (xs, x) => xs.some((y) => $eq(x, y)),
  find: (xs, f) => { for (const x of xs) if (f(x)) return Some(x); return None; },
  index_of: (xs, x) => { const i = xs.findIndex((y) => $eq(x, y)); return i < 0 ? None : Some(i); },
  any: (xs, f) => xs.some((x) => f(x)),
  all: (xs, f) => xs.every((x) => f(x)),
  count: (xs, f) => xs.filter((x) => f(x)).length,
  join: (xs, sep) => xs.join(sep),
  sum: (xs) => xs.reduce((a, b) => $int(a + b), 0),
  sum_float: (xs) => xs.reduce((a, b) => a + b, 0),
  take: (xs, n) => xs.slice(0, Math.max(0, n)),
  drop: (xs, n) => xs.slice(Math.max(0, n)),
  flat_map: (xs, f) => xs.flatMap((x) => f(x)),
  repeat: (x, n) => Array.from({ length: Math.max(0, n) }, () => x),
  unique: (xs) => xs.filter((x, i) => xs.findIndex((y) => $eq(x, y)) === i),
};
const $string = {
  len: (s) => $chars(s).length,
  is_empty: (s) => s.length === 0,
  upper: (s) => s.toUpperCase(),
  lower: (s) => s.toLowerCase(),
  trim: (s) => s.trim(),
  split: (s, sep) => (sep === "" ? $chars(s) : s.split(sep)),
  lines: (s) => s.split(/\r?\n/),
  chars: $chars,
  contains: (s, p) => s.includes(p),
  starts_with: (s, p) => s.startsWith(p),
  ends_with: (s, p) => s.endsWith(p),
  replace: (s, a, b) => (a === "" ? s : s.split(a).join(b)),
  slice: (s, from, to) => $chars(s).slice(Math.max(0, from), Math.max(0, to)).join(""),
  index_of: (s, p) => { const i = s.indexOf(p); return i < 0 ? None : Some($chars(s.slice(0, i)).length); },
  repeat: (s, n) => s.repeat(Math.max(0, n)),
  reverse: (s) => $chars(s).reverse().join(""),
  pad_left: (s, w, fill = " ") => { let out = s; while ($chars(out).length < w && fill) out = fill + out; return out; },
  pad_right: (s, w, fill = " ") => { let out = s; while ($chars(out).length < w && fill) out = out + fill; return out; },
};
// A dict is a Map from a key's text form to [key, value], so equal keys always match.
const $key = (k) => (typeof k === "string" ? "s" + k : typeof k === "number" ? "n" + k : "v" + $repr(k));
const $dict = {
  empty: () => new Map(),
  from_list: (pairs) => new Map(pairs.map((p) => [$key(p.key), [p.key, p.value]])),
  set: (d, k, v) => new Map(d).set($key(k), [k, v]),
  get: (d, k) => { const e = d.get($key(k)); return e ? Some(e[1]) : None; },
  has: (d, k) => d.has($key(k)),
  remove: (d, k) => { const m = new Map(d); m.delete($key(k)); return m; },
  keys: (d) => [...d.values()].map((e) => e[0]),
  values: (d) => [...d.values()].map((e) => e[1]),
  entries: (d) => [...d.values()].map(([key, value]) => ({ key, value })),
  size: (d) => d.size,
};
const $math = {
  sqrt: Math.sqrt,
  pow: Math.pow,
  abs: Math.abs,
  floor: (x) => $int(Math.floor(x)),
  ceil: (x) => $int(Math.ceil(x)),
  round: (x) => $int(Math.round(x)),
  min: Math.min,
  max: Math.max,
  random: () => Math.random(),
};
const $int_mod = {
  abs: (n) => Math.abs(n),
  min: (a, b) => Math.min(a, b),
  max: (a, b) => Math.max(a, b),
  clamp: (n, lo, hi) => Math.min(Math.max(n, lo), hi),
};
const $node = (name) => {
  const p = globalThis.process;
  if (!p || typeof p.getBuiltinModule !== "function") $panic("Files are only available in Node or Bun, not in the browser.");
  return p.getBuiltinModule(name);
};
const $fs = {
  read_file: (path) => $ext_res(() => $node("node:fs").readFileSync(path, "utf8")),
  write_file: (path, text) => $ext_res(() => { $node("node:fs").writeFileSync(path, text); }),
  exists: (path) => { try { return $node("node:fs").existsSync(path); } catch { return false; } },
};
const $os = {
  args: () => (globalThis.process ? globalThis.process.argv.slice(2) : []),
  env: (name) => $ext_opt(globalThis.process ? globalThis.process.env[name] : undefined),
};

function $report(e) {
  if (e instanceof PlainPanic) {
    $io.err(`Plain stopped: ${e.message}` + (e.loc ? `\n  at ${e.loc}` : ""));
  } else if (e instanceof RangeError && /call stack/i.test(e.message)) {
    $io.err("Plain stopped: a function called itself too many times (stack overflow).");
  } else {
    $io.err("JavaScript error: " + (e && e.stack ? e.stack : String(e)));
  }
}
function $main(main) {
  try {
    const r = main();
    if (r && r.$ === "Err") {
      $io.err("Error: " + $show(r._0));
      $io.exit(1);
    }
  } catch (e) {
    $report(e);
    $io.exit(1);
  }
}
function $runTests(file, tests) {
  let failed = 0;
  $io.out(file);
  for (const [name, run] of tests) {
    try {
      run();
      $io.out(`  ok    ${name}`);
    } catch (e) {
      failed++;
      $io.out(`  FAIL  ${name}`);
      const msg = e instanceof PlainPanic ? e.message + (e.loc ? `\n  at ${e.loc}` : "") : String(e && e.stack ? e.stack : e);
      $io.out(msg.split("\n").map((l) => "        " + l).join("\n"));
    }
  }
  $io.out(`  ${tests.length - failed} passed, ${failed} failed`);
  if (failed) $io.exit(1);
}
// ---- end of runtime ----
