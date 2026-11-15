import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";

interface Props<T> {
  rows: T[];
  rowHeight: number;
  rowKey: (row: T, index: number) => string;
  renderRow: (row: T, index: number) => ReactNode;
  className?: string;
  overscan?: number;
}

/**
 * 固定行高虚拟列表。只渲染可视窗口 + overscan，10 万行时 DOM 节点数保持在几十个。
 * 垂直滚动容器本身也是组件根节点；行使用绝对定位，避免大列表的布局抖动。
 */
export function VirtualList<T>({ rows, rowHeight, rowKey, renderRow, className, overscan = 10 }: Props<T>) {
  const ref = useRef<HTMLDivElement>(null);
  const [range, setRange] = useState({ start: 0, end: 0 });

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      const viewport = el.clientHeight;
      const start = Math.max(0, Math.floor(el.scrollTop / rowHeight) - overscan);
      const end = Math.min(rows.length, Math.ceil((el.scrollTop + viewport) / rowHeight) + overscan);
      setRange((prev) => (prev.start === start && prev.end === end ? prev : { start, end }));
    };
    const onScroll = () => {
      if (frame === 0) frame = window.requestAnimationFrame(update);
    };
    update();
    el.addEventListener("scroll", onScroll, { passive: true });
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => {
      el.removeEventListener("scroll", onScroll);
      observer.disconnect();
      if (frame !== 0) window.cancelAnimationFrame(frame);
    };
  }, [rows.length, rowHeight, overscan]);

  useEffect(() => {
    const el = ref.current;
    if (el && el.scrollTop > rows.length * rowHeight) {
      el.scrollTop = 0;
    }
  }, [rows, rowHeight]);

  return (
    <div ref={ref} className={className}>
      <div style={{ height: rows.length * rowHeight, position: "relative", minWidth: 1240 }}>
        {rows.slice(range.start, range.end).map((row, i) => {
          const index = range.start + i;
          return (
            <div
              key={rowKey(row, index)}
              className="vrow"
              style={{ position: "absolute", top: index * rowHeight, left: 0, right: 0, height: rowHeight }}
            >
              {renderRow(row, index)}
            </div>
          );
        })}
      </div>
    </div>
  );
}
