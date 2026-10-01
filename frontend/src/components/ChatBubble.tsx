import { Avatar, Message as KitMessage } from "@chatscope/chat-ui-kit-react";
import { Pin } from "lucide-react";
import type { Message } from "../lib/types";

// Reuse the kit's layout and safe text renderer, adapting RIVER's data/theme.
export function ChatBubble({ message, own, avatarUrl, pinned }: {
  message: Message;
  own: boolean;
  avatarUrl?: string;
  pinned: boolean;
}) {
  const time = new Date(message.at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  return (
    <KitMessage className="river-chat-bubble"
      model={{ direction: own ? "outgoing" : "incoming", position: "single", type: "text" }}
      avatarPosition={own ? "tr" : "tl"}
    >
      <Avatar name={message.name} className="chat-avatar">
        <span>{message.name.slice(0, 1).toUpperCase()}</span>
        {avatarUrl && <img src={avatarUrl} alt="" onError={(e) => { e.currentTarget.style.display = "none"; }} />}
      </Avatar>
      <KitMessage.Header>
        {!own && <span className="chat-sender-name">{message.name}</span>}
        <time className="chat-message-time" dateTime={message.at}>{time}</time>
      </KitMessage.Header>
      <KitMessage.TextContent text={message.text} />
      {pinned && <KitMessage.Footer><span className="chat-pinned-tag"><Pin size={11} />已置顶</span></KitMessage.Footer>}
    </KitMessage>
  );
}
