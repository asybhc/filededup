// 更新相关文案的占位符一致性检查。
//
// 覆盖范围：
//   - web/src/i18n/en-app.ts 中「自动更新提示」整段，以及所有含
//     更新/版本/备份/安装/下载/跳过/检查 字样且带占位符的词典键；
//   - web/src 下所有 .ts/.tsx 里对 t("…") / tr("…") 的调用。
//
// 判据：
//   1. 中文键里的占位符集合 == 英文译文的占位符集合；
//   2. 每个带占位符的更新文案，调用处传入的参数集合必须与占位符集合完全相等；
//   3. 调用处多传参数、少传参数、或第二参数不是对象字面量都算失败；
//   4. 不带占位符的更新文案，调用处不得传参数。
//
// 运行：npm test（等价 node scripts/check-update-placeholders.mjs）
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const srcRoot = path.join(webRoot, "src");
const enAppPath = path.join(srcRoot, "i18n", "en-app.ts");

// 只覆盖更新入口与对话框：词典里的「自动更新提示」整段，加上顶栏徽标等
// 明确以更新/检查更新/下载并安装开头的键。避免把“跳过回收站项”等无关文案混进来。
const UPDATE_KEY = /^(有更新|检查更新|发现新版本|下载并安装|确认覆盖|更新|跳过此版本)/;
const PLACEHOLDER = /\{([a-zA-Z][a-zA-Z0-9_]*)\}/g;

function placeholders(text) {
  const out = new Set();
  for (const m of text.matchAll(PLACEHOLDER)) out.add(m[1]);
  return out;
}

function sameSet(a, b) {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}

function describeSet(s) {
  return [...s].sort().join(",") || "(none)";
}

function leadingComments(sourceText, node) {
  const ranges = ts.getLeadingCommentRanges(sourceText, node.getFullStart()) || [];
  return ranges.map((r) => sourceText.slice(r.pos, r.end)).join("\n");
}

// ---- 1. 解析英文词典，提取更新相关键 ----
const enText = fs.readFileSync(enAppPath, "utf8");
const enFile = ts.createSourceFile("en-app.ts", enText, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
let enObject = null;
function findEnObject(node) {
  if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.name.text === "enApp") {
    if (node.initializer && ts.isObjectLiteralExpression(node.initializer)) enObject = node.initializer;
  }
  ts.forEachChild(node, findEnObject);
}
findEnObject(enFile);
if (!enObject) {
  console.error("FAIL: 未在 en-app.ts 找到 enApp 对象字面量");
  process.exit(1);
}

const updateEntries = [];
let inUpdateSection = false;
for (const prop of enObject.properties) {
  if (!ts.isPropertyAssignment(prop)) continue;
  const key = ts.isStringLiteral(prop.name) ? prop.name.text : prop.name.getText(enFile);
  const value = ts.isStringLiteral(prop.initializer) ? prop.initializer.text : null;
  if (value === null) continue;

  const comments = leadingComments(enText, prop);
  if (comments.includes("自动更新提示")) inUpdateSection = true;
  else if (inUpdateSection && comments.trim() !== "") inUpdateSection = false;

  if (inUpdateSection || UPDATE_KEY.test(key)) {
    updateEntries.push({ key, value, line: enFile.getLineAndCharacterOfPosition(prop.getStart()).line + 1 });
  }
}

if (updateEntries.length === 0) {
  console.error("FAIL: 未提取到任何更新相关词典键");
  process.exit(1);
}


// ---- 2. 扫描所有调用点 ----
const callSites = [];
function walkDir(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walkDir(full);
    else if (entry.isFile() && /\.(ts|tsx)$/.test(entry.name)) scanFile(full);
  }
}

function paramKeysOf(arg, sf) {
  if (!arg) return { keys: new Set(), unknown: false };
  if (!ts.isObjectLiteralExpression(arg)) return { keys: new Set(), unknown: true };
  const keys = new Set();
  let unknown = false;
  for (const p of arg.properties) {
    if (ts.isPropertyAssignment(p)) {
      const name = ts.isIdentifier(p.name) || ts.isStringLiteral(p.name) ? p.name.text : p.name.getText(sf);
      keys.add(name);
    } else if (ts.isShorthandPropertyAssignment(p)) {
      keys.add(p.name.text);
    } else {
      unknown = true; // spread / 计算属性，静态无法判定
    }
  }
  return { keys, unknown };
}

function scanFile(file) {
  const text = fs.readFileSync(file, "utf8");
  const kind = file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind);
  function visit(node) {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression)) {
      const callee = node.expression.text;
      if (callee === "t" || callee === "tr") {
        const first = node.arguments[0];
        if (first && (ts.isStringLiteral(first) || ts.isNoSubstitutionTemplateLiteral(first))) {
          const key = first.text;
          const params = paramKeysOf(node.arguments[1], sf);
          const pos = sf.getLineAndCharacterOfPosition(node.getStart());
          callSites.push({
            key,
            file: path.relative(webRoot, file),
            line: pos.line + 1,
            params: params.keys,
            unknown: params.unknown,
          });
        }
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(sf);
}

walkDir(srcRoot);

// ---- 3. 逐条核对 ----
const updateKeys = new Set(updateEntries.map((e) => e.key));
const failures = [];
let checkedKeys = 0;
let checkedCalls = 0;

for (const entry of updateEntries) {
  checkedKeys++;
  const zh = placeholders(entry.key);
  const en = placeholders(entry.value);
  if (!sameSet(zh, en)) {
    failures.push(
      `词典 ${entry.line}: "${entry.key}" 中文占位符 {${describeSet(zh)}} != 英文 {${describeSet(en)}}`,
    );
  }

  const sites = callSites.filter((c) => c.key === entry.key);
  if (zh.size > 0 && sites.length === 0) {
    failures.push(`词典 ${entry.line}: "${entry.key}" 含占位符 {${describeSet(zh)}}，但没有任何 t()/tr() 调用`);
    continue;
  }
  for (const site of sites) {
    checkedCalls++;
    if (site.unknown) {
      failures.push(`${site.file}:${site.line}: "${entry.key}" 第二参数不是可静态核对的字面量对象`);
      continue;
    }
    if (!sameSet(zh, site.params)) {
      failures.push(
        `${site.file}:${site.line}: "${entry.key}" 占位符 {${describeSet(zh)}} != 调用参数 {${describeSet(site.params)}}`,
      );
    }
  }
}

if (failures.length > 0) {
  console.error(`FAIL: ${failures.length} 处占位符不一致`);
  for (const f of failures) console.error("  - " + f);
  process.exit(1);
}

console.log(
  `OK: 更新文案占位符一致（${checkedKeys} 个词典键，${checkedCalls} 个调用点，${callSites.length} 个 t()/tr() 调用已扫描）`,
);
