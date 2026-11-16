// 界面语言：中文为源文案，英文用“中文原文 -> English”的查表方式。
// 这样调用点写 t("原文") 就能得到两种语言，缺词时回退中文，不会被吞成空白。
// 语言状态不进入 React state 树：设置保存后调用 setLang，所有订阅组件立即重渲染。

import { useSyncExternalStore } from "react";
import enCommon from "./i18n/en-common";
import enApp from "./i18n/en-app";
import enOrganize from "./i18n/en-organize";
import enLanzou from "./i18n/en-lanzou";
import enMisc from "./i18n/en-misc";

export type Lang = "zh" | "en";
export type TParams = Record<string, string | number>;

const EN: Record<string, string> = {
  ...enCommon,
  ...enApp,
  ...enOrganize,
  ...enLanzou,
  ...enMisc,
};

let current: Lang = "zh";
const listeners = new Set<() => void>();
const warned = new Set<string>();

export function getLang(): Lang {
  return current;
}

export function setLang(next: Lang) {
  if (next !== "zh" && next !== "en") return;
  if (current === next) return;
  current = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

// 翻译单条文案。params 用 {name} 占位，避免把数字/路径拼进字典键。
export function tr(text: string, params?: TParams): string {
  if (current === "zh") return text;
  let out = EN[text];
  if (out === undefined) {
    if (import.meta.env.DEV && !warned.has(text)) {
      warned.add(text);
      console.warn("[i18n] missing English translation:", text);
    }
    out = text;
  }
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      out = out.split(`{${key}}`).join(String(value));
    }
  }
  return out;
}

// 组件里用这个取 t：它订阅语言变化，切换语言时该组件会重新渲染。
export function useT(): typeof tr {
  useSyncExternalStore(subscribe, getLang, getLang);
  return tr;
}
