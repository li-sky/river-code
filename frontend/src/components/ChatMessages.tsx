import { useEffect, useRef, useState } from "react";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { Copy, MessageCircle, Pin, PinOff, Undo2 } from "lucide-react";
import type { Message, Player } from "../lib/types";
import { ChatBubble } from "./ChatBubble";

export function ChatMessages({ messages, players, userId, isHost, connected, pinnedId, now, send, onError }: {
  messages: Message[];
  players: Player[];
  userId: string;
  isHost: boolean;
  connected: boolean;
  pinnedId?: string;
  now: number;
  send: (command: Record<string, unknown>) => void;
  onError: (error: unknown) => void;
}) {
  const [activeId, setActiveId] = useState<string | null>(null);
  const [multiTouch, setMultiTouch] = useState(false);
  const [copied, setCopied] = useState(false);
  const list = useRef<HTMLDivElement>(null);
  const end = useRef<HTMLDivElement>(null);
  const triggers = useRef(new Map<string, HTMLSpanElement>());
  const restoreFocus = useRef(true);

  useEffect(() => {
    end.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
  }, [messages[messages.length - 1]?.id]);

  // Radix cancels long press on move/up/cancel. Disable pending triggers
  // when a second touch is present as well.
  useEffect(() => {
    const touches = new Set<number>();
    const down = (e: PointerEvent) => {
      if (e.pointerType === "mouse") return;
      touches.add(e.pointerId);
      if (touches.size > 1) { setMultiTouch(true); setActiveId(null); }
    };
    const up = (e: PointerEvent) => {
      touches.delete(e.pointerId);
      if (!touches.size) setMultiTouch(false);
    };
    document.addEventListener("pointerdown", down, true);
    document.addEventListener("pointerup", up, true);
    document.addEventListener("pointercancel", up, true);
    return () => {
      document.removeEventListener("pointerdown", down, true);
      document.removeEventListener("pointerup", up, true);
      document.removeEventListener("pointercancel", up, true);
    };
  }, []);

  useEffect(() => {
    if (activeId && !messages.some((m) => m.id === activeId && !m.recalled)) setActiveId(null);
  }, [activeId, messages]);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);

  return (
    <>
      <div className="chat-messages" ref={list} onScroll={() => setActiveId(null)}>
        {messages.length ? messages.map((m) => {
          const own = m.userId === userId;
          const canRecall = own && now >= Date.parse(m.at) && now - Date.parse(m.at) <= 120_000;
          if (m.recalled) return (
            <div className="chat-recalled" key={m.id} data-message-id={m.id}>
              {own ? "你" : m.name}撤回了一条消息
              <time dateTime={m.at}>{new Date(m.at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}</time>
            </div>
          );
          const act = (type: string) => { if (connected) send({ type, messageId: m.id }); };
          return (
            <div className={`chat-message ${own ? "own-message" : ""}`} key={m.id} data-message-id={m.id}>
              <ContextMenu.Root open={activeId === m.id} onOpenChange={(open) => {
                if (open) restoreFocus.current = true;
                setActiveId((current) => open ? m.id : current === m.id ? null : current);
              }} modal={false}>
                <ContextMenu.Trigger
                  className="chat-message-trigger" tabIndex={0} role="button" disabled={multiTouch}
                  ref={(element) => { if (element) triggers.current.set(m.id, element); else triggers.current.delete(m.id); }}
                  aria-label={`${m.name}的消息：${m.text}`} aria-haspopup="menu" aria-expanded={activeId === m.id}
                  onPointerDown={(e) => { e.stopPropagation(); if (!e.isPrimary || e.button !== 0) e.preventDefault(); }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " " || e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
                      e.preventDefault();
                      const r = e.currentTarget.querySelector(".cs-message__content")?.getBoundingClientRect() || e.currentTarget.getBoundingClientRect();
                      e.currentTarget.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: r.left, clientY: r.bottom }));
                    }
                  }}
                >
                  <ChatBubble message={m} own={own} avatarUrl={players.find((p) => p.id === m.userId)?.avatarUrl} pinned={pinnedId === m.id} />
                </ContextMenu.Trigger>
                <ContextMenu.Portal container={list.current?.parentElement}>
                  <ContextMenu.Content
                    className="chat-message-menu" aria-label="消息操作" collisionPadding={8}
                    onPointerDown={(e) => e.stopPropagation()} onWheel={(e) => e.stopPropagation()}
                    onInteractOutside={(e) => {
                      // Touch release generates a click on the original trigger.
                      if (triggers.current.get(m.id)?.contains(e.target as Node)) e.preventDefault();
                      else restoreFocus.current = false;
                    }}
                    onCloseAutoFocus={(e) => { e.preventDefault(); if (restoreFocus.current) triggers.current.get(m.id)?.focus({ preventScroll: true }); }}
                  >
                    <ContextMenu.Item onSelect={async () => {
                      try { await navigator.clipboard.writeText(m.text); setCopied(true); }
                      catch { onError(new Error("复制失败，请检查浏览器剪贴板权限")); }
                    }}><Copy size={16} />复制文字</ContextMenu.Item>
                    {isHost && <ContextMenu.Item disabled={!connected} onSelect={() => act(pinnedId === m.id ? "unpin_message" : "pin_message")}>
                      {pinnedId === m.id ? <PinOff size={16} /> : <Pin size={16} />}{pinnedId === m.id ? "取消置顶" : "置顶消息"}
                    </ContextMenu.Item>}
                    {canRecall && <ContextMenu.Item className="recall-action" disabled={!connected} onSelect={() => act("recall_message")}><Undo2 size={16} />撤回消息</ContextMenu.Item>}
                  </ContextMenu.Content>
                </ContextMenu.Portal>
              </ContextMenu.Root>
            </div>
          );
        }) : (
          <div className="empty-chat"><MessageCircle size={30} /><p>一声招呼，牌局更有温度。</p><span>消息也会在头像气泡中出现</span></div>
        )}
        <div ref={end} />
      </div>
      <span className="sr-only" role="status">{copied ? "消息已复制" : ""}</span>
    </>
  );
}
